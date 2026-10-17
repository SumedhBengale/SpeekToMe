// Privileged, narrowly scoped modifier shortcut watcher for native Linux
// packages. It prints only P/R transitions for one selected chord. No raw
// keycodes or text leave this process.
#define _GNU_SOURCE
#include <errno.h>
#include <fcntl.h>
#include <glob.h>
#include <grp.h>
#include <linux/input.h>
#include <poll.h>
#include <pwd.h>
#include <signal.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/prctl.h>
#include <sys/types.h>
#include <unistd.h>

#define MAX_INPUTS 128

enum chord { CTRL_META, CTRL_SHIFT, CTRL_ALT, RIGHT_ALT };

static volatile sig_atomic_t stop_requested = 0;

static void on_signal(int signal_number) {
    (void)signal_number;
    stop_requested = 1;
}

static bool chord_active(enum chord chord, const bool down[KEY_MAX + 1]) {
    const bool ctrl = down[KEY_LEFTCTRL] || down[KEY_RIGHTCTRL];
    const bool meta = down[KEY_LEFTMETA] || down[KEY_RIGHTMETA];
    const bool shift = down[KEY_LEFTSHIFT] || down[KEY_RIGHTSHIFT];
    const bool alt = down[KEY_LEFTALT] || down[KEY_RIGHTALT];
    switch (chord) {
    case CTRL_META: return ctrl && meta;
    case CTRL_SHIFT: return ctrl && shift;
    case CTRL_ALT: return ctrl && alt;
    case RIGHT_ALT: return down[KEY_RIGHTALT];
    }
    return false;
}

static bool selected_chord(const char *value, enum chord *chord) {
    if (strcmp(value, "ctrl-meta") == 0) *chord = CTRL_META;
    else if (strcmp(value, "ctrl-shift") == 0) *chord = CTRL_SHIFT;
    else if (strcmp(value, "ctrl-alt") == 0) *chord = CTRL_ALT;
    else if (strcmp(value, "right-alt") == 0) *chord = RIGHT_ALT;
    else return false;
    return true;
}

static int drop_root(void) {
    const char *uid_text = getenv("PKEXEC_UID");
    if (!uid_text || !*uid_text) return -1;
    char *end = NULL;
    errno = 0;
    unsigned long uid_number = strtoul(uid_text, &end, 10);
    if (errno || !end || *end || uid_number > UINT32_MAX) return -1;
    uid_t uid = (uid_t)uid_number;
    struct passwd *account = getpwuid(uid);
    if (!account) return -1;
    // All input devices are already open. Restrict the remainder of the
    // watcher to the requesting user's identity.
    if (setgroups(0, NULL) != 0 || setgid(account->pw_gid) != 0 || setuid(uid) != 0) return -1;
    return 0;
}

int main(int argc, char **argv) {
    if (argc != 2 || getuid() != 0) return 2;
    enum chord chord;
    if (!selected_chord(argv[1], &chord)) return 2;
    if (prctl(PR_SET_PDEATHSIG, SIGTERM) != 0 || getppid() == 1) return 2;
    signal(SIGTERM, on_signal);
    signal(SIGINT, on_signal);

    glob_t inputs = {0};
    if (glob("/dev/input/event*", 0, NULL, &inputs) != 0) return 3;
    struct pollfd fds[MAX_INPUTS];
    size_t count = 0;
    for (size_t i = 0; i < inputs.gl_pathc && count < MAX_INPUTS; i++) {
        int fd = open(inputs.gl_pathv[i], O_RDONLY | O_NONBLOCK | O_CLOEXEC);
        if (fd < 0) continue;
        fds[count].fd = fd;
        fds[count].events = POLLIN;
        fds[count].revents = 0;
        count++;
    }
    globfree(&inputs);
    if (count == 0 || drop_root() != 0) return 3;
    if (puts("READY") == EOF || fflush(stdout) != 0) return 3;

    bool down[KEY_MAX + 1] = {0};
    bool active = false;
    while (!stop_requested) {
        int ready = poll(fds, count, 1000);
        if (ready < 0) {
            if (errno == EINTR) continue;
            break;
        }
        if (ready == 0) continue;
        for (size_t i = 0; i < count; i++) {
            if (!(fds[i].revents & POLLIN)) continue;
            struct input_event event;
            while (read(fds[i].fd, &event, sizeof(event)) == sizeof(event)) {
                if (event.type != EV_KEY || event.code > KEY_MAX || event.value == 2) continue;
                down[event.code] = event.value == 1;
                bool now_active = chord_active(chord, down);
                if (now_active == active) continue;
                active = now_active;
                if (puts(active ? "P" : "R") == EOF || fflush(stdout) != 0) {
                    stop_requested = 1;
                    break;
                }
            }
        }
    }
    for (size_t i = 0; i < count; i++) close(fds[i].fd);
    return 0;
}
