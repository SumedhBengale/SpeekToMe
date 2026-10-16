#include "transcribe.h"
#include <chrono>
#include <cstdio>
#include <cstdlib>
#include <fstream>
#include <sstream>
#include <string>
#include <vector>

static double now() {
    return std::chrono::duration<double>(std::chrono::steady_clock::now().time_since_epoch()).count();
}
static std::string quoted(const std::string &s) {
    std::string out = "\"";
    for (unsigned char c : s) {
        if (c == '\\' || c == '"') { out += '\\'; out += c; }
        else if (c < 32) { char b[8]; std::snprintf(b, sizeof b, "\\u%04x", c); out += b; }
        else out += c;
    }
    return out + "\"";
}
struct Clip { std::string id; std::vector<float> pcm; };
int main(int argc, char **argv) {
    if (argc != 7) { std::fprintf(stderr, "model manifest batch iterations context threads\n"); return 2; }
    int batch = std::atoi(argv[3]), iterations = std::atoi(argv[4]);
    if (batch < 1 || iterations < 1) return 2;
    std::vector<Clip> clips;
    std::ifstream manifest(argv[2]);
    std::string line;
    while (std::getline(manifest, line)) {
        auto tab = line.find('\t');
        if (tab == std::string::npos) return 2;
        Clip c; c.id = line.substr(0, tab);
        std::ifstream f(line.substr(tab+1), std::ios::binary | std::ios::ate);
        if (!f || f.tellg() <= 0 || f.tellg() % 4 != 0) return 2;
        c.pcm.resize(static_cast<size_t>(f.tellg()) / 4);
        f.seekg(0); f.read(reinterpret_cast<char *>(c.pcm.data()), c.pcm.size()*4);
        clips.push_back(std::move(c));
    }
    if (clips.empty()) return 2;
    transcribe_model_load_params lp; transcribe_model_load_params_init(&lp);
    lp.backend = TRANSCRIBE_BACKEND_CUDA;
    transcribe_session_params sp; transcribe_session_params_init(&sp);
    sp.n_ctx = std::atoi(argv[5]); sp.n_threads = std::atoi(argv[6]);
    transcribe_session *session = nullptr;
    double start = now();
    auto status = transcribe_open(argv[1], &lp, &sp, &session);
    std::printf("{\"event\":\"load\",\"seconds\":%.6f,\"status\":%s}\n", now()-start, quoted(transcribe_status_string(status)).c_str());
    std::fflush(stdout);
    if (status != TRANSCRIBE_OK) return 1;
    std::printf("{\"event\":\"device\",\"backend\":%s,\"batch\":%d,\"context\":%d,\"threads\":%d}\n",
        quoted(transcribe_model_backend(transcribe_get_model(session))).c_str(), batch, sp.n_ctx, sp.n_threads);
    transcribe_run_params rp; transcribe_run_params_init(&rp);
    rp.language = "en"; rp.timestamps = TRANSCRIBE_TIMESTAMPS_NONE;
    size_t cursor = 0; int failed = 0;
    // Iteration -1 is a full inference warmup, kept in the log but excluded from throughput.
    for (int it = -1; it < iterations; ++it) {
        std::vector<const float *> data; std::vector<int> sizes; std::vector<size_t> indexes;
        for (int b=0; b<batch; ++b) {
            size_t i = (cursor+b) % clips.size(); indexes.push_back(i);
            data.push_back(clips[i].pcm.data()); sizes.push_back(static_cast<int>(clips[i].pcm.size()));
        }
        start = now();
        status = batch == 1 ? transcribe_run(session, data[0], sizes[0], &rp)
                            : transcribe_run_batch(session, data.data(), sizes.data(), batch, &rp);
        double seconds = now()-start;
        for (int b=0; b<batch; ++b) {
            auto row_status = batch == 1 || status != TRANSCRIBE_OK ? status : transcribe_batch_status(session,b);
            const char *text = batch == 1 ? transcribe_full_text(session) : transcribe_batch_full_text(session,b);
            bool success = row_status == TRANSCRIBE_OK && text && *text;
            std::printf("{\"event\":\"result\",\"iteration\":%d,\"warmup\":%s,\"batch\":%d,\"row\":%d,\"id\":%s,\"audio_seconds\":%.6f,\"batch_wall_seconds\":%.6f,\"status\":%s,\"success\":%s,\"text\":%s}\n",
                it,it < 0 ? "true":"false",batch,b,quoted(clips[indexes[b]].id).c_str(),sizes[b]/16000.0,seconds,
                quoted(transcribe_status_string(row_status)).c_str(),success?"true":"false",quoted(text?text:"").c_str());
            if (!success) ++failed;
        }
        std::fflush(stdout);
        if (status != TRANSCRIBE_OK) break;
        if (it >= 0) cursor += batch;
    }
    transcribe_session_free(session);
    return failed ? 1 : 0;
}
