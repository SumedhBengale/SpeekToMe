//! Cached WebKit XPC helpers are launched by launchd (PPid 1), not direct Rust
//! children. Address their services in our process's bootstrap namespace only.
//! No root access, private WebKit APIs or global browser-process matching.

pub fn services(output: &str, pid: u32) -> Vec<(i32, String)> {
    if output.lines().next() != Some(format!("pid/{pid} = {{").as_str()) {
        return Vec::new();
    }
    let mut in_services = false;
    let mut result = Vec::new();
    for line in output.lines() {
        if line == "\tservices = {" {
            in_services = true;
            continue;
        }
        if !in_services {
            continue;
        }
        if line == "\t}" {
            break;
        }
        let fields = line.split_whitespace().collect::<Vec<_>>();
        if fields.len() != 3 || fields[0].parse::<u32>().unwrap_or(0) == 0 {
            continue;
        }
        let name = fields[2];
        if [
            "com.apple.WebKit.WebContent",
            "com.apple.WebKit.Networking",
            "com.apple.WebKit.GPU",
        ]
        .iter()
        .any(|prefix| {
            name == *prefix
                || name.strip_prefix(prefix).is_some_and(|suffix| {
                    suffix.starts_with('.')
                        && suffix[1..]
                            .chars()
                            .all(|c| c.is_ascii_hexdigit() || c == '-')
                })
        }) {
            if let Ok(pid) = fields[0].parse::<i32>() {
                if pid > 0 {
                    result.push((pid, name.to_owned()));
                }
            }
        }
    }
    result
}

#[cfg(target_os = "macos")]
pub fn cached_services() -> Vec<(i32, String)> {
    let pid = std::process::id();
    let Ok(output) = std::process::Command::new("/bin/launchctl")
        .args(["print", &format!("pid/{pid}")])
        .output()
    else {
        return Vec::new();
    };
    if !output.status.success() {
        eprintln!(
            "[BROWSER] Cannot inspect own XPC namespace: {}",
            String::from_utf8_lossy(&output.stderr)
                .lines()
                .next()
                .unwrap_or("launchctl failed")
        );
        return Vec::new();
    }
    // launchctl also prints environment metadata: never log its raw output.
    services(&String::from_utf8_lossy(&output.stdout), pid)
}

#[cfg(target_os = "macos")]
pub fn terminate_cached(services: &[(i32, String)]) -> usize {
    #[link(name = "proc")]
    unsafe extern "C" {
        fn proc_pidpath(pid: i32, buffer: *mut u8, size: u32) -> i32;
    }
    unsafe extern "C" {
        fn kill(pid: i32, signal: i32) -> i32;
    }
    let mut stopped = 0;
    for (pid, service) in services {
        // Refresh membership immediately before signalling; XPC workers can
        // exit between listing and cleanup. Never use a global name match.
        if !cached_services()
            .iter()
            .any(|candidate| candidate == &(*pid, service.clone()))
        {
            continue;
        }
        let mut path = [0u8; 4096];
        let length = unsafe { proc_pidpath(*pid, path.as_mut_ptr(), path.len() as u32) };
        if length <= 0 {
            continue;
        }
        let executable = String::from_utf8_lossy(&path[..length as usize]);
        let executable = executable.trim_end_matches('\0');
        if !executable.contains("/WebKit.framework/")
            || ![
                "com.apple.WebKit.GPU",
                "com.apple.WebKit.Networking",
                "com.apple.WebKit.WebContent",
            ]
            .contains(&executable.rsplit('/').next().unwrap_or(""))
        {
            continue;
        }
        // launchctl kill requires extra launchd signalling privilege even for
        // our XPC services. POSIX signalling enforces the same-user boundary.
        if unsafe { kill(*pid, 15) } == 0 {
            stopped += 1;
        } else {
            eprintln!(
                "[BROWSER] Could not stop owned {service}: {}",
                std::io::Error::last_os_error()
            );
        }
    }
    stopped
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn only_live_webkit_services_in_the_exact_host_namespace_are_selected() {
        let output = "pid/42 = {\n\tservices = {\n\t\t101 - com.apple.WebKit.GPU.AB12-34CD\n\t\t102 - com.apple.WebKit.WebContent\n\t\t103 - com.apple.WebKit.Networking.ABCDEF\n\t\t0 - com.apple.WebKit.GPU\n\t\t104 - com.apple.WebKit.GPUevil\n\t\t105 - com.apple.WebKit.GPU.unknown/service\n\t\t106 - com.apple.other.Helper\n\t}\n\tother = {\n\t\t107 - com.apple.WebKit.GPU\n\t}\n}";
        assert_eq!(
            services(output, 42),
            vec![
                (101, "com.apple.WebKit.GPU.AB12-34CD".into()),
                (102, "com.apple.WebKit.WebContent".into()),
                (103, "com.apple.WebKit.Networking.ABCDEF".into())
            ]
        );
        assert!(services(output, 43).is_empty());
        assert!(services("", 42).is_empty());
    }
}
