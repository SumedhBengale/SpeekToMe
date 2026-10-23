//! One capture coordinator for every native shortcut adapter.
use crate::gestures::{Action, Mode, Recognizer, Route};
use std::sync::{
    atomic::{AtomicBool, Ordering},
    mpsc, Arc, Mutex, OnceLock,
};
use std::time::{Duration, Instant};

static TIMER: OnceLock<std::thread::Thread> = OnceLock::new();
// Settings publishes this before taking the coordinator lock. Capture must never
// lock AppConfig while holding Controller (Settings takes them in reverse order).
static RECORDING_ACCOUNT: Mutex<(Option<String>, Option<String>)> = Mutex::new((None, None));
pub fn publish_account(config: &crate::AppConfig) {
    *RECORDING_ACCOUNT.lock().unwrap_or_else(|e| e.into_inner()) =
        (config.session_token.clone(), config.api_url.clone());
}
const PORTAL_GRACE: Duration = Duration::from_millis(20);

// Expire selection mode even if the settings webview closes unexpectedly.
static SELECTING: Mutex<Option<Instant>> = Mutex::new(None);

pub fn selecting() -> bool {
    SELECTING
        .lock()
        .unwrap_or_else(|e| e.into_inner())
        .is_some_and(|until| Instant::now() < until)
}

pub fn set_selecting(active: bool) {
    *SELECTING.lock().unwrap_or_else(|e| e.into_inner()) = Some(
        Instant::now()
            + if active {
                Duration::from_secs(30)
            } else {
                Duration::from_millis(250)
            },
    );
    if active {
        cancel();
    }
}

struct Controller {
    recognizer: Recognizer,
    settings: (u32, Mode, bool),
    generation: u64,
    active: Option<u64>,
    stop: Option<mpsc::Sender<bool>>,
    route: Option<mpsc::Sender<Route>>,
    cancelled: Option<Arc<AtomicBool>>,
}

fn settings() -> (u32, Mode, bool) {
    (
        crate::ACTIVE_HOTKEY_COMBO.load(Ordering::SeqCst),
        if crate::ACTIVE_HOTKEY_MODE.load(Ordering::SeqCst) == 1 {
            Mode::Toggle
        } else {
            Mode::Hold
        },
        crate::ACTIVE_COMMAND_MODE.load(Ordering::SeqCst),
    )
}

fn controller() -> &'static Mutex<Controller> {
    static CONTROLLER: OnceLock<Mutex<Controller>> = OnceLock::new();
    CONTROLLER.get_or_init(|| {
        let settings = settings();
        Mutex::new(Controller {
            recognizer: Recognizer::new(settings.1, settings.2),
            settings,
            generation: 0,
            active: None,
            stop: None,
            route: None,
            cancelled: None,
        })
    })
}

// The lease follows the actual work, including asynchronous batch requests and
// streamed transformations. A new gesture cannot change its routing or hide its
// overlay before that work finishes. No OS hook waits for audio/network/clipboard.
struct SessionLife {
    id: u64,
    history_id: String,
    origin_token: Option<String>,
    origin_url: String,
    cancelled: Arc<AtomicBool>,
}
impl Drop for SessionLife {
    fn drop(&mut self) {
        let mut c = controller().lock().unwrap_or_else(|e| e.into_inner());
        if c.active == Some(self.id) {
            c.active = None;
            c.stop = None;
            c.route = None;
            c.cancelled = None;
            c.recognizer.complete();
            crate::win32::hide_overlay();
            crate::win32::hide_preview();
            crate::win32::play_chime(false);
        }
    }
}

pub struct RecordingSession {
    route: mpsc::Receiver<Route>,
    life: Arc<SessionLife>,
}

#[derive(Clone)]
pub struct RecordingContext {
    pub is_command_mode: bool,
    pub selected_text: Option<String>,
    life: Arc<SessionLife>,
}

impl RecordingSession {
    pub fn is_cancelled(&self) -> bool {
        self.life.cancelled.load(Ordering::SeqCst)
    }
    pub fn keep_alive(&self) -> impl Drop {
        self.life.clone()
    }

    // Called after the microphone starts: its buffer preserves the initial
    // audio while the single/double decision is pending. The WS handshake only
    // follows a final route, so Command Mode is resolved before audio is sent.
    pub fn resolve(self) -> Option<RecordingContext> {
        self.resolve_with(crate::keyboard::get_selected_text)
    }

    fn resolve_with(self, selection: impl FnOnce() -> Option<String>) -> Option<RecordingContext> {
        let route = self.route.recv().ok()?;
        if self.is_cancelled() {
            return None;
        }
        let is_command_mode = route == Route::Command;
        let selected_text = if is_command_mode { selection() } else { None };
        if self.is_cancelled() {
            return None;
        }
        Some(RecordingContext {
            is_command_mode,
            selected_text,
            life: self.life,
        })
    }
}

impl RecordingContext {
    pub fn history_identity(&self) -> (&str, Option<&str>, &str) {
        (
            &self.life.history_id,
            self.life.origin_token.as_deref(),
            &self.life.origin_url,
        )
    }
    pub fn capture_finished(&self) {
        let mut c = controller().lock().unwrap_or_else(|e| e.into_inner());
        if c.active == Some(self.life.id) {
            c.recognizer.capture_finished();
            c.stop = None;
        }
    }
}

impl Controller {
    fn apply(&mut self, actions: Vec<Action>) {
        for action in actions {
            match action {
                Action::Capture => {
                    self.generation += 1;
                    self.active = Some(self.generation);
                    let (stop, rx) = mpsc::channel();
                    let (route, route_rx) = mpsc::channel();
                    self.stop = Some(stop);
                    self.route = Some(route);
                    let cancelled = Arc::new(AtomicBool::new(false));
                    self.cancelled = Some(cancelled.clone());
                    let (origin_token, origin_url) = {
                        let account = RECORDING_ACCOUNT.lock().unwrap_or_else(|e| e.into_inner());
                        (
                            account.0.clone(),
                            account
                                .1
                                .clone()
                                .unwrap_or_else(|| "https://api.speekto.me".into()),
                        )
                    };
                    let session = RecordingSession {
                        route: route_rx,
                        life: Arc::new(SessionLife {
                            id: self.generation,
                            history_id: uuid::Uuid::new_v4().to_string(),
                            origin_token,
                            origin_url,
                            cancelled,
                        }),
                    };
                    crate::win32::set_preview_text("");
                    crate::win32::set_preview_state(crate::win32::PreviewState::Listening);
                    crate::win32::show_overlay();
                    crate::win32::play_chime(true);
                    std::thread::spawn(move || crate::audio::recording_thread(rx, session));
                }
                Action::Select(route) => {
                    eprintln!("[SHORTCUT] session={:?} route={route:?}", self.active);
                    if route == Route::Command {
                        crate::win32::set_preview_text("✨ Command Mode (Listening...)");
                    }
                    if let Some(tx) = self.route.take() {
                        let _ = tx.send(route);
                    }
                }
                Action::Stop {
                    submit,
                    held_in_toggle,
                } => {
                    // Disconnect an unresolved route on abort. The audio worker
                    // exits without opening a transcription WebSocket.
                    self.route = None;
                    if !submit {
                        if let Some(cancelled) = &self.cancelled {
                            cancelled.store(true, Ordering::SeqCst);
                        }
                    }
                    if let Some(stop) = self.stop.take() {
                        let _ = stop.send(submit);
                    }
                    if submit {
                        crate::win32::set_preview_state(crate::win32::PreviewState::Processing);
                        if held_in_toggle {
                            crate::win32::show_tray_notification("Hands-Free is selected",
                                "A hold gesture was detected. Recording was submitted on release; tap to start and stop in Hands-Free mode.",
                                crate::win32::NotificationLevel::Warning);
                        }
                    } else {
                        crate::win32::hide_overlay();
                        crate::win32::hide_preview();
                    }
                }
            }
        }
    }

    fn sync_settings(&mut self) {
        let updated = settings();
        if updated != self.settings {
            let actions = self.recognizer.cancel();
            self.apply(actions);
            self.settings = updated;
            self.recognizer = Recognizer::new(updated.1, updated.2);
            if self.active.is_some() {
                self.recognizer.capture_finished();
            }
        }
    }
}

pub fn start() {
    static STARTED: OnceLock<()> = OnceLock::new();
    STARTED.get_or_init(|| {
        let worker = std::thread::spawn(|| loop {
            if crate::lifecycle::is_quitting() {
                break;
            }
            let mut c = controller().lock().unwrap_or_else(|e| e.into_inner());
            c.sync_settings();
            // Leave time for the Linux portal's timestamp-ordered signal batch.
            let actions = c.recognizer.tick(Instant::now() - PORTAL_GRACE);
            c.apply(actions);
            let wait = c
                .recognizer
                .deadline()
                .map(|at| (at + PORTAL_GRACE).saturating_duration_since(Instant::now()));
            drop(c);
            if let Some(wait) = wait {
                std::thread::park_timeout(wait);
            } else {
                std::thread::park();
            }
        });
        let _ = TIMER.set(worker.thread().clone());
    });
}

pub fn settings_changed() {
    if let Some(timer) = TIMER.get() {
        timer.unpark();
    }
}

#[cfg(target_os = "windows")]
pub fn is_capturing() -> bool {
    controller()
        .lock()
        .unwrap_or_else(|e| e.into_inner())
        .recognizer
        .is_capturing()
}

pub fn on_edge(pressed: bool) {
    on_edge_at(pressed, Instant::now());
}

pub fn on_edge_at(pressed: bool, now: Instant) {
    if crate::lifecycle::is_quitting() || selecting() {
        return;
    }
    // Always accept releases, including after auth expires or a meeting starts.
    if pressed
        && (!crate::HOTKEY_ALLOWED.load(Ordering::SeqCst)
            || crate::ACTIVE_MEETING
                .lock()
                .unwrap_or_else(|e| e.into_inner())
                .is_some()
            || !crate::ACTIVE_HAS_SESSION_TOKEN.load(Ordering::SeqCst))
    {
        return;
    }
    let mut c = controller().lock().unwrap_or_else(|e| e.into_inner());
    c.sync_settings();
    let actions = c.recognizer.edge(pressed, now);
    c.apply(actions);
    drop(c);
    settings_changed();
}

pub fn cancel() {
    let mut c = controller().lock().unwrap_or_else(|e| e.into_inner());
    let actions = c.recognizer.cancel();
    c.apply(actions);
}

#[cfg(test)]
mod tests {
    use super::*;

    fn session() -> (mpsc::Sender<Route>, RecordingSession) {
        let (tx, rx) = mpsc::channel();
        (
            tx,
            RecordingSession {
                route: rx,
                life: Arc::new(SessionLife {
                    id: u64::MAX,
                    history_id: uuid::Uuid::new_v4().to_string(),
                    origin_token: None,
                    origin_url: "https://api.speekto.me".into(),
                    cancelled: Arc::new(AtomicBool::new(false)),
                }),
            },
        )
    }

    #[test]
    fn dictation_never_copies_the_selection() {
        let (tx, session) = session();
        tx.send(Route::Dictation).unwrap();
        let context = session
            .resolve_with(|| panic!("dictation must not copy"))
            .unwrap();
        assert!(!context.is_command_mode);
        assert!(context.selected_text.is_none());
    }

    #[test]
    fn command_context_and_lease_follow_async_work() {
        let (tx, session) = session();
        tx.send(Route::Command).unwrap();
        tx.send(Route::Dictation).unwrap(); // Cannot change a resolved route.
        let context = session
            .resolve_with(|| Some("selected text".into()))
            .unwrap();
        let transform = context.clone();
        assert_eq!(Arc::strong_count(&context.life), 2);
        drop(context);
        assert!(transform.is_command_mode);
        assert_eq!(transform.selected_text.as_deref(), Some("selected text"));
        assert_eq!(Arc::strong_count(&transform.life), 1);
    }

    #[test]
    fn cancellation_during_selection_never_dispatches_a_command() {
        let (tx, session) = session();
        let cancelled = session.life.cancelled.clone();
        tx.send(Route::Command).unwrap();
        let context = session.resolve_with(|| {
            cancelled.store(true, Ordering::SeqCst);
            Some("selected text".into())
        });
        assert!(context.is_none());
    }

    #[test]
    fn dropping_stream_disconnects_reader_and_releases_its_session() {
        use futures_util::{SinkExt, StreamExt};
        use tokio_tungstenite::tungstenite::Message;
        let listener = std::net::TcpListener::bind("127.0.0.1:0").unwrap();
        let address = listener.local_addr().unwrap();
        listener.set_nonblocking(true).unwrap();
        let (closed_tx, closed_rx) = mpsc::channel();
        let server = std::thread::spawn(move || {
            tokio::runtime::Builder::new_current_thread()
                .enable_all()
                .build()
                .unwrap()
                .block_on(async move {
                    let listener = tokio::net::TcpListener::from_std(listener).unwrap();
                    let (tcp, _) = listener.accept().await.unwrap();
                    let mut ws = tokio_tungstenite::accept_async(tcp).await.unwrap();
                    let start = ws.next().await.unwrap().unwrap();
                    assert!(
                        serde_json::from_str::<serde_json::Value>(&start.into_text().unwrap())
                            .unwrap()
                            .get("enhance")
                            .is_none()
                    );
                    ws.send(Message::Text(
                        r#"{"type":"session_ready","mode":"continuous_stream"}"#.into(),
                    ))
                    .await
                    .unwrap();
                    let closed = tokio::time::timeout(Duration::from_secs(2), ws.next()).await;
                    closed_tx
                        .send(matches!(
                            closed,
                            Ok(None) | Ok(Some(Err(_))) | Ok(Some(Ok(Message::Close(_))))
                        ))
                        .unwrap();
                });
        });
        let (tx, session) = session();
        tx.send(Route::Command).unwrap();
        let context = session.resolve_with(|| None).unwrap();
        let life = Arc::downgrade(&context.life);
        let handle = crate::stream::start_streaming_session(
            format!("http://{address}"),
            "local-test".into(),
            String::new(),
            "en".into(),
            context,
        )
        .unwrap();
        assert!(handle.interim_text().is_empty()); // Status text is never a transcript fallback.
        assert!(life.upgrade().is_some());
        drop(handle);
        assert!(closed_rx.recv_timeout(Duration::from_secs(3)).unwrap());
        server.join().unwrap();
        let until = Instant::now() + Duration::from_secs(1);
        while life.upgrade().is_some() && Instant::now() < until {
            std::thread::sleep(Duration::from_millis(5));
        }
        assert!(life.upgrade().is_none());
    }

    #[test]
    fn aborted_first_tap_has_no_route_or_selection_capture() {
        let (tx, session) = session();
        drop(tx);
        assert!(session
            .resolve_with(|| panic!("aborted gesture must not copy"))
            .is_none());
    }
}
