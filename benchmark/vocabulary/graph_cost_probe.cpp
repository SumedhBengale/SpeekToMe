// Isolated CPU cost probe for the ContextBiasingTree in the pinned S3 source.
// Synthetic token IDs measure graph bookkeeping, not ASR accuracy or GPU cost.
// Compile against the extracted src/asr/decoders/context_biasing.cpp and header.
#include "context_biasing.h"
#include <algorithm>
#include <chrono>
#include <cmath>
#include <iostream>
#include <stdexcept>
#include <unordered_map>
#include <vector>

using nemo_speech::asr::ContextBiasingTree;
using Clock = std::chrono::steady_clock;
static volatile double sink = 0;

static double us(Clock::time_point start) {
    return std::chrono::duration<double, std::micro>(Clock::now() - start).count();
}

static void run(int count, bool shared) {
    std::vector<std::vector<int>> phrases;
    for (int i = 0; i < count; ++i)
        phrases.push_back({shared ? i % 32 : i, 2000 + i, 4000 + i, 6000 + i, 8000 + i});
    std::vector<float> scores(count, 1.0f);
    ContextBiasingTree tree;
    std::vector<double> builds;
    for (int i = 0; i < 31; ++i) {
        auto start = Clock::now();
        tree.build(phrases, scores, 2.0f);
        builds.push_back(us(start));
    }
    std::sort(builds.begin(), builds.end());
    // A sequence of root, partial, completed and abandoned matches; repeated
    // deterministic states let us examine an immutable per-state score cache.
    std::vector<int> states;
    int state = 0;
    for (const auto& phrase : phrases) {
        states.push_back(0);
        state = 0;
        for (int token : phrase) {
            state = tree.advance(state, token);
            states.push_back(state);
        }
        states.push_back(tree.advance(state, 12000));
    }
    std::unordered_map<int, ContextBiasingTree::StepBoost> cache;
    auto start = Clock::now();
    for (int s : states) {
        if (cache.find(s) == cache.end()) cache.emplace(s, tree.step_boost(s));
    }
    double cache_build = us(start);
    size_t pairs = 0;
    for (const auto& entry : cache) pairs += entry.second.specials.size();
    // Validate equality independently of unordered-map traversal order.
    for (const auto& entry : cache) {
        auto fresh = tree.step_boost(entry.first);
        auto saved = entry.second;
        std::sort(fresh.specials.begin(), fresh.specials.end());
        std::sort(saved.specials.begin(), saved.specials.end());
        if (fresh.base != saved.base || fresh.specials != saved.specials)
            throw std::runtime_error("cached score mismatch");
    }
    constexpr int repeats = 5;
    const int calls = std::max(14000, static_cast<int>(states.size()) * 2);
    std::vector<double> fresh_times, cached_times;
    for (int r = 0; r < repeats; ++r) {
        start = Clock::now();
        for (int i = 0; i < calls; ++i) {
            auto sb = tree.step_boost(states[i % states.size()]);
            sink = sink + sb.base + sb.specials.size();
        }
        fresh_times.push_back(us(start) / calls);
        start = Clock::now();
        for (int i = 0; i < calls; ++i) {
            const auto& sb = cache.at(states[i % states.size()]);
            sink = sink + sb.base + sb.specials.size();
        }
        cached_times.push_back(us(start) / calls);
    }
    std::sort(fresh_times.begin(), fresh_times.end());
    std::sort(cached_times.begin(), cached_times.end());
    std::cout << "{\"phrases\":" << count << ",\"root_shape\":\""
              << (shared ? "32_shared_starts" : "unique_starts")
              << "\",\"tokens_per_phrase\":5,\"build_median_us\":" << builds[15]
              << ",\"build_p95_us\":" << builds[29]
              << ",\"step_median_us\":" << fresh_times[2]
              << ",\"cached_step_median_us\":" << cached_times[2]
              << ",\"cache_prepare_us\":" << cache_build
              << ",\"cached_states\":" << cache.size()
              << ",\"cache_pair_payload_bytes\":" << pairs * sizeof(std::pair<int, float>)
              << ",\"calls_per_repeat\":" << calls
              << ",\"cache_equality_passed\":true}\n";
}

int main() {
    for (int count : {100, 1000}) {
        run(count, true);
        run(count, false);
    }
}
