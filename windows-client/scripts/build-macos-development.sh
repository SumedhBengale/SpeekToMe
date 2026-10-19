#!/bin/sh
# Build a local ad-hoc signed app for testing. Public releases use Tauri's
# Developer ID signing/notarization pipeline, not this development signature.
set -eu
[ "$(uname -s)" = Darwin ] || { echo 'Run this script on macOS.' >&2; exit 1; }
cd "$(dirname "$0")/.."
export PATH="$HOME/.cargo/bin:$HOME/Tools/cmake/CMake.app/Contents/bin:$PATH"
cargo build --locked
app="$PWD/target/debug/bundle/macos/SpeekTo.Me.app"
mkdir -p "$app/Contents/MacOS" "$app/Contents/Resources"
cp target/debug/speektome "$app/Contents/MacOS/speektome"
cp src-tauri/icons/icon.icns "$app/Contents/Resources/icon.icns"
cp src-tauri/Info.plist "$app/Contents/Info.plist"
plist="$app/Contents/Info.plist"
/usr/libexec/PlistBuddy -c 'Add :CFBundleExecutable string speektome' "$plist"
/usr/libexec/PlistBuddy -c 'Add :CFBundleIdentifier string me.speekto.SpeekToMe' "$plist"
/usr/libexec/PlistBuddy -c 'Add :CFBundleName string SpeekTo.Me' "$plist"
/usr/libexec/PlistBuddy -c 'Add :CFBundleDisplayName string SpeekTo.Me' "$plist"
/usr/libexec/PlistBuddy -c 'Add :CFBundlePackageType string APPL' "$plist"
/usr/libexec/PlistBuddy -c 'Add :CFBundleShortVersionString string 1.0.0' "$plist"
/usr/libexec/PlistBuddy -c 'Add :CFBundleVersion string 1' "$plist"
/usr/libexec/PlistBuddy -c 'Add :CFBundleIconFile string icon.icns' "$plist"
/usr/libexec/PlistBuddy -c 'Add :LSMinimumSystemVersion string 13.0' "$plist"
/usr/libexec/PlistBuddy -c 'Add :NSHighResolutionCapable bool true' "$plist"
codesign --force --sign - --options runtime --entitlements src-tauri/Entitlements.plist "$app"
codesign --verify --strict "$app"
printf '%s\n' "$app"
