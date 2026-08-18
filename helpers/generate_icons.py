import os
from PIL import Image, ImageDraw

def main():
    svg_content = """<svg xmlns="http://www.w3.org/2000/svg"
     width="128"
     height="128"
     viewBox="0 0 1254 1254">
    <g fill="#FC5B90">
        <!-- Bar 1 -->
        <rect x="265" y="484" width="96" height="291" rx="48"/>
        <!-- Bar 2 -->
        <rect x="418" y="375" width="98" height="507" rx="49"/>
        <!-- Bar 3 -->
        <rect x="578" y="239" width="100" height="773" rx="50"/>
        <!-- Bar 4 -->
        <rect x="740" y="342" width="99" height="565" rx="49.5"/>
        <!-- Bar 5 -->
        <rect x="898" y="483" width="95" height="293" rx="47.5"/>
    </g>
</svg>
"""

    svg_destinations = [
        r"e:\Projects\SpeekToMe\assets\icons\logo.svg",
        r"e:\Projects\SpeekToMe\website\logo.svg",
        r"e:\Projects\SpeekToMe\website\favicon.svg",
        r"e:\Projects\SpeekToMe\website\assets\logo.svg",
        r"e:\Projects\SpeekToMe\website\public\logo.svg",
        r"e:\Projects\SpeekToMe\website\public\favicon.svg",
        r"e:\Projects\SpeekToMe\website\public\assets\logo.svg",
        r"e:\Projects\SpeekToMe\windows-client\frontend\assets\icons\logo.svg",
    ]

    for p in svg_destinations:
        os.makedirs(os.path.dirname(p), exist_ok=True)
        with open(p, "w", encoding="utf-8") as f:
            f.write(svg_content)
        print("Updated SVG:", p)

    # Create 8x supersampled master image for ultra-smooth anti-aliased rendering
    scale = 8
    size = 1254 * scale
    master = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    draw = ImageDraw.Draw(master)

    fill_color = (252, 91, 144, 255)  # #FC5B90

    bars = [
        (265 * scale, 484 * scale, (265 + 96) * scale, (484 + 291) * scale, 48 * scale),
        (418 * scale, 375 * scale, (418 + 98) * scale, (375 + 507) * scale, 49 * scale),
        (578 * scale, 239 * scale, (578 + 100) * scale, (239 + 773) * scale, 50 * scale),
        (740 * scale, 342 * scale, (740 + 99) * scale, (342 + 565) * scale, 49.5 * scale),
        (898 * scale, 483 * scale, (898 + 95) * scale, (483 + 293) * scale, 47.5 * scale),
    ]

    for x1, y1, x2, y2, rx in bars:
        draw.rounded_rectangle([x1, y1, x2, y2], radius=rx, fill=fill_color)

    # Generate master 1024x1024
    master_1024 = master.resize((1024, 1024), Image.Resampling.LANCZOS)
    master_1024_path = r"e:\Projects\SpeekToMe\master_icon.png"
    master_1024.save(master_1024_path, "PNG", optimize=True)
    print("Updated master 1024x1024:", master_1024_path)

    # Generate 512x512 PNGs
    png_512 = master.resize((512, 512), Image.Resampling.LANCZOS)
    for p in [
        r"e:\Projects\SpeekToMe\assets\icons\logo.png",
        r"e:\Projects\SpeekToMe\windows-client\frontend\assets\icons\logo.png",
        r"e:\Projects\SpeekToMe\windows-client\src-tauri\icons\icon.png",
    ]:
        os.makedirs(os.path.dirname(p), exist_ok=True)
        png_512.save(p, "PNG", optimize=True)
        print("Updated 512x512 PNG:", p)

    # Generate specific Tauri PNG sizes
    sizes_map = {
        (32, 32): r"e:\Projects\SpeekToMe\windows-client\src-tauri\icons\32x32.png",
        (64, 64): r"e:\Projects\SpeekToMe\windows-client\src-tauri\icons\64x64.png",
        (128, 128): r"e:\Projects\SpeekToMe\windows-client\src-tauri\icons\128x128.png",
        (256, 256): r"e:\Projects\SpeekToMe\windows-client\src-tauri\icons\128x128@2x.png",
        (30, 30): r"e:\Projects\SpeekToMe\windows-client\src-tauri\icons\Square30x30Logo.png",
        (44, 44): r"e:\Projects\SpeekToMe\windows-client\src-tauri\icons\Square44x44Logo.png",
        (71, 71): r"e:\Projects\SpeekToMe\windows-client\src-tauri\icons\Square71x71Logo.png",
        (89, 89): r"e:\Projects\SpeekToMe\windows-client\src-tauri\icons\Square89x89Logo.png",
        (107, 107): r"e:\Projects\SpeekToMe\windows-client\src-tauri\icons\Square107x107Logo.png",
        (142, 142): r"e:\Projects\SpeekToMe\windows-client\src-tauri\icons\Square142x142Logo.png",
        (150, 150): r"e:\Projects\SpeekToMe\windows-client\src-tauri\icons\Square150x150Logo.png",
        (284, 284): r"e:\Projects\SpeekToMe\windows-client\src-tauri\icons\Square284x284Logo.png",
        (310, 310): r"e:\Projects\SpeekToMe\windows-client\src-tauri\icons\Square310x310Logo.png",
        (50, 50): r"e:\Projects\SpeekToMe\windows-client\src-tauri\icons\StoreLogo.png",
    }

    for (w, h), p in sizes_map.items():
        resized = master.resize((w, h), Image.Resampling.LANCZOS)
        resized.save(p, "PNG", optimize=True)
        print(f"Updated {w}x{h} PNG: {p}")

    # Generate multi-resolution ICO file
    ico_path = r"e:\Projects\SpeekToMe\windows-client\src-tauri\icons\icon.ico"
    ico_sizes = [(16, 16), (24, 24), (32, 32), (48, 48), (64, 64), (128, 128), (256, 256)]
    master_1024.save(ico_path, format="ICO", sizes=ico_sizes)
    print("Updated multi-resolution ICO:", ico_path)

if __name__ == "__main__":
    main()
