#!/usr/bin/env python3
import os
import sys
import argparse
import requests
import io
import json
import time

def test_connection(server_url: str, session_token: str):
    print(f"Testing connection to {server_url} with provided session token...")
    headers = {"Authorization": f"Bearer {session_token}"}
    try:
        r = requests.get(f"{server_url}/health", timeout=5)
        print(f"Health check status: {r.status_code} ({r.text.strip()})")
    except Exception as e:
        print(f"Note: Health check request: {e}")

    # Test auth
    try:
        r = requests.get(f"{server_url}/auth/me", headers=headers, timeout=5)
        if r.status_code == 200:
            user_data = r.json()
            print(f"Authentication SUCCESS! User: {user_data.get('email', 'Unknown')} (Plan: {user_data.get('plan_tier', 'free')})")
            return True
        else:
            print(f"Authentication FAILED: Status {r.status_code} - {r.text}")
            return False
    except Exception as e:
        print(f"Auth request error: {e}")
        return False

def main():
    parser = argparse.ArgumentParser(description="SpeekTo.Me Open ASR Leaderboard Runner")
    parser.add_argument("--server-url", default=os.environ.get("SPEEKTOME_SERVER_URL", "http://127.0.0.1:8000"), help="SpeekTo.Me Server Base URL")
    parser.add_argument("--token", default=os.environ.get("SPEEKTOME_SESSION_TOKEN", ""), help="Session Token for SpeekTo.Me")
    parser.add_argument("--dataset", default="librispeech", help="Dataset name, e.g., librispeech, ami_cleaned")
    parser.add_argument("--split", default="test.clean", help="Split name, e.g., test.clean, test")
    parser.add_argument("--dataset-path", default="hf-audio/open-asr-leaderboard", help="Dataset repo on Hugging Face")
    parser.add_argument("--model-variant", default="full-pipeline", choices=["full-pipeline", "raw-asr"], help="speektome variant: full-pipeline (speech + LLM) or raw-asr (speech only)")
    parser.add_argument("--max-samples", type=int, default=None, help="Limit number of evaluation samples")
    parser.add_argument("--max-workers", type=int, default=4, help="Concurrent workers for evaluation")
    parser.add_argument("--test-auth", action="store_true", help="Only test authentication and connection")

    args = parser.parse_args()

    if not args.token:
        print("ERROR: Session token not provided. Pass --token or set SPEEKTOME_SESSION_TOKEN environment variable.")
        sys.exit(1)

    os.environ["SPEEKTOME_SERVER_URL"] = args.server_url
    os.environ["SPEEKTOME_SESSION_TOKEN"] = args.token

    if args.test_auth:
        ok = test_connection(args.server_url, args.token)
        sys.exit(0 if ok else 1)

    # Test connection first
    if not test_connection(args.server_url, args.token):
        print("Proceeding with benchmark run...")

    # Run leaderboard eval
    repo_api_dir = os.path.abspath(os.path.join(os.path.dirname(__file__), "open_asr_leaderboard", "api"))
    repo_root = os.path.abspath(os.path.join(os.path.dirname(__file__), "open_asr_leaderboard"))
    sys.path.insert(0, repo_api_dir)
    sys.path.insert(0, repo_root)

    model_name = f"speektome/{args.model_variant}"
    print(f"\n==========================================")
    print(f"Starting Open ASR Leaderboard Benchmark")
    print(f"Model:          {model_name}")
    print(f"Dataset:        {args.dataset} ({args.split})")
    print(f"Dataset Path:   {args.dataset_path}")
    print(f"Max Samples:    {args.max_samples or 'ALL'}")
    print(f"Workers:        {args.max_workers}")
    print(f"==========================================\n")

    # Import run_eval functions
    from run_eval import transcribe_dataset
    import evaluate
    from normalizer.eval_utils import score_results

    transcribe_dataset(
        dataset_path=args.dataset_path,
        dataset=args.dataset,
        split=args.split,
        model_name=model_name,
        use_url=False,
        max_samples=args.max_samples,
        max_workers=args.max_workers,
    )

    results_dir = os.path.join(repo_api_dir, "results", model_name.replace("/", "-"))
    os.makedirs(results_dir, exist_ok=True)
    
    # Move results to model folder and score
    import glob, shutil
    raw_results = glob.glob(os.path.join(repo_api_dir, "results", f"MODEL_{model_name.replace('/', '-')}_DATASET_*.jsonl"))
    for f in raw_results:
        dest = os.path.join(results_dir, os.path.basename(f))
        shutil.move(f, dest)

    print("\n--- Scoring Benchmark Results ---")
    score_results(results_dir, model_name)

if __name__ == "__main__":
    main()
