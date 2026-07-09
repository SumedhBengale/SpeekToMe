# 1. Build React dashboard
Push-Location dashboard
$env:VITE_API_URL="https://api.speekto.me"
npm run build
Remove-Item Env:\VITE_API_URL
Pop-Location

# 2. Copy root marketing pages & static files to dist/
Copy-Item -Path index.html, about.html, pricing.html, privacy.html, refund.html, terms.html, styles.css, script.js -Destination dist/ -Force
if (Test-Path assets) {
    Copy-Item -Path assets -Destination dist/ -Recurse -Force
}


# 3. Deploy the unified dist directory to Cloudflare Pages
npx wrangler pages deploy dist --project-name speekto-me
