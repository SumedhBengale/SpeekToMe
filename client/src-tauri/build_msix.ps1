# Create layout directory
$msixDir = "e:\Projects\SpeekToMe\client\target\msix_dir"
$assetsDir = "$msixDir\assets"
$outDir = "e:\Projects\SpeekToMe\client\target\release\bundle\msix"

New-Item -ItemType Directory -Path $msixDir -Force | Out-Null
New-Item -ItemType Directory -Path $assetsDir -Force | Out-Null
New-Item -ItemType Directory -Path $outDir -Force | Out-Null

# Copy main binary
Copy-Item "e:\Projects\SpeekToMe\client\target\release\speektome.exe" "$msixDir\speektome.exe" -Force

# Copy icons
Copy-Item "e:\Projects\SpeekToMe\client\src-tauri\icons\128x128.png" "$assetsDir\Square150x150Logo.png" -Force
Copy-Item "e:\Projects\SpeekToMe\client\src-tauri\icons\32x32.png" "$assetsDir\Square44x44Logo.png" -Force
Copy-Item "e:\Projects\SpeekToMe\client\src-tauri\icons\128x128.png" "$assetsDir\StoreLogo.png" -Force

# Package using makeappx.exe
$makeappx = "C:\Program Files (x86)\Windows Kits\10\bin\10.0.26100.0\x64\makeappx.exe"
& $makeappx pack /d $msixDir /p "$outDir\SpeekTo.Me_1.0.0_x64.msix" /o
