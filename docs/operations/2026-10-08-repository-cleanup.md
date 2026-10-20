# Repository cleanup, 2026-10-08

Removed generated Rust/Go outputs, dependency/build caches, Cloudflare local state, runtime language caches, obsolete standalone audio clients, an unused training-model configuration, old LLM/Bedrock experiment scripts and raw one-off fleet benchmark scripts/logs/results.

Retained application source and tests, existing uncommitted server/client work, local meeting recordings and databases, useful helpers, GitHub Actions benchmark/deploy inputs, current gateway/controller deployment sources, and concise benchmark reports/configuration.

Dedicated CPU diarization deployment is in [deployment/diarization-server](../../deployment/diarization-server/README.md). Dependencies/models are downloaded during setup; credentials and model weights are excluded from Git. The cleanup does not redeploy AWS services.

Local disk usage fell from approximately 37GiB to 198MiB (including Git history). Builds must recreate removed caches and binaries. Other ongoing server/client edits remain outside this cleanup commit.
