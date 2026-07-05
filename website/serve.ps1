# 1. Build React dashboard
Push-Location dashboard
npm run build
Pop-Location

# 2. Copy root marketing pages & static files to dist/
Copy-Item -Path index.html, about.html, pricing.html, privacy.html, refund.html, terms.html, styles.css, script.js -Destination dist/ -Force
if (Test-Path assets) {
    Copy-Item -Path assets -Destination dist/ -Recurse -Force
}


# 3. Serve the unified dist directory locally
npx wrangler pages dev dist --port 8080
