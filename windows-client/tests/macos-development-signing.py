"""macOS integration check: a grant requirement must survive a changed build.

Uses disposable app bundles and codesign verification; never touches TCC grants.
"""
import pathlib
import plistlib
import subprocess
import tempfile

root = pathlib.Path(__file__).resolve().parents[1]


def run(*args):
    return subprocess.run(args, check=True, capture_output=True, text=True)


with tempfile.TemporaryDirectory(prefix="speektome-signing-") as directory:
    work = pathlib.Path(directory)
    requirement = None
    hashes = []
    for version in (1, 2):
        app = work / f"build-{version}" / "SpeekTo.Me.app"
        binary = app / "Contents/MacOS/speektome"
        binary.parent.mkdir(parents=True)
        source = work / "main.c"
        source.write_text(f"int main(void) {{ return {version}; }}\n")
        run("xcrun", "clang", str(source), "-o", str(binary))
        with (app / "Contents/Info.plist").open("wb") as handle:
            plistlib.dump({"CFBundleExecutable": "speektome",
                          "CFBundleIdentifier": "me.speekto.SpeekToMe",
                          "CFBundlePackageType": "APPL"}, handle)
        run("sh", str(root / "scripts/sign-macos-development.sh"), str(app))
        display = run("codesign", "--display", "--verbose=4", "--requirements", "-", str(app))
        details = display.stdout + display.stderr
        hashes.append(next(line for line in details.splitlines() if line.startswith("CDHash=")))
        if requirement is None:
            requirement = next(line.lstrip("# ").removeprefix("designated => ")
                               for line in details.splitlines() if line.lstrip("# ").startswith("designated => "))
        run("codesign", "--verify", "--strict", "-R", "=" + requirement, str(app))
    assert hashes[0] != hashes[1], "Fixture must actually change the executable signature"
    print("PASS: both changed builds satisfy the first build's permission identity")
