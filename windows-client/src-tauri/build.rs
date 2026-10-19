use std::{env, path::PathBuf, process::Command};

fn main() {
    if env::var("CARGO_CFG_TARGET_OS").as_deref() == Ok("macos") {
        println!("cargo:rerun-if-changed=native/macos.m");
        let out = PathBuf::from(env::var_os("OUT_DIR").unwrap());
        let target = env::var("TARGET").unwrap();
        let arch = if target.starts_with("aarch64") {
            "arm64"
        } else {
            "x86_64"
        };
        let status = Command::new("xcrun")
            .args([
                "clang",
                "-arch",
                arch,
                "-mmacosx-version-min=13.0",
                "-fobjc-arc",
                "-fblocks",
                "-c",
                "native/macos.m",
                "-o",
            ])
            .arg(out.join("macos.o"))
            .status()
            .expect("macOS native bridge requires Xcode command-line tools");
        assert!(status.success(), "macOS native bridge compilation failed");
        let status = Command::new("xcrun")
            .args(["ar", "crs"])
            .arg(out.join("libspeektome_macos.a"))
            .arg(out.join("macos.o"))
            .status()
            .unwrap();
        assert!(status.success(), "macOS native bridge archive failed");
        println!("cargo:rustc-link-search=native={}", out.display());
        println!("cargo:rustc-link-lib=static=speektome_macos");
        for framework in [
            "AppKit",
            "CoreGraphics",
            "CoreText",
            "CoreFoundation",
            "ApplicationServices",
            "AVFoundation",
            "Carbon",
            "UserNotifications",
            "ScreenCaptureKit",
            "CoreMedia",
            "CoreAudio",
            "AudioToolbox",
            "ServiceManagement",
        ] {
            println!("cargo:rustc-link-lib=framework={framework}");
        }
    }
    tauri_build::build();
}
