# 1. Build unified React + Vite application
$env:VITE_API_URL="https://api.speekto.me"
npm run build
Remove-Item Env:\VITE_API_URL

# 2. Deploy the dist directory to Cloudflare Pages
npx wrangler pages deploy dist --project-name speekto-me
