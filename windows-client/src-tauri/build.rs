fn main() {
    std::env::set_var("CMAKE_POLICY_VERSION_MINIMUM", "3.5");
    tauri_build::build();
}
