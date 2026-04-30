// Prevents an additional console window on Windows in release builds.
#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod permissions;

use serde::Serialize;
use std::io::{Read, Write};
use std::net::TcpStream;
use std::sync::Mutex;
use std::time::Duration;

use tauri::{Emitter, Manager, RunEvent};
use tauri_plugin_shell::process::{CommandChild, CommandEvent};
use tauri_plugin_shell::ShellExt;

// Holds the spawned pan-agent sidecar so we can kill it on app exit.
struct SidecarHandle(Mutex<Option<CommandChild>>);

#[derive(Serialize)]
struct ApiFetchResponse {
    status: u16,
    body: String,
}

#[derive(Clone, Serialize)]
struct ApiStreamEvent {
    kind: String,
    status: Option<u16>,
    data: Option<String>,
    error: Option<String>,
}

fn emit_api_stream(
    app: &tauri::AppHandle,
    stream_id: &str,
    kind: &str,
    status: Option<u16>,
    data: Option<String>,
    error: Option<String>,
) {
    let _ = app.emit(
        &format!("api-stream:{stream_id}"),
        ApiStreamEvent {
            kind: kind.to_string(),
            status,
            data,
            error,
        },
    );
}

#[tauri::command]
fn api_fetch(
    method: String,
    path: String,
    body: Option<String>,
) -> Result<ApiFetchResponse, String> {
    if !path.starts_with('/') || path.starts_with("//") || path.contains("://") {
        return Err("invalid local API path".to_string());
    }

    let method = method.trim().to_ascii_uppercase();
    let body = body.unwrap_or_default();
    let content_length = body.len();
    let request = format!(
        "{method} {path} HTTP/1.1\r\nHost: 127.0.0.1:8642\r\nContent-Type: application/json\r\nAccept: application/json\r\nConnection: close\r\nContent-Length: {content_length}\r\n\r\n{body}"
    );

    let mut stream = TcpStream::connect("127.0.0.1:8642")
        .map_err(|err| format!("connect 127.0.0.1:8642 failed: {err}"))?;
    let _ = stream.set_read_timeout(Some(Duration::from_secs(300)));
    let _ = stream.set_write_timeout(Some(Duration::from_secs(10)));
    stream
        .write_all(request.as_bytes())
        .map_err(|err| format!("write local API request failed: {err}"))?;

    let mut raw = Vec::new();
    stream
        .read_to_end(&mut raw)
        .map_err(|err| format!("read local API response failed: {err}"))?;
    let response = String::from_utf8_lossy(&raw);
    let (headers, body) = response
        .split_once("\r\n\r\n")
        .ok_or_else(|| "invalid local API response".to_string())?;
    let status = headers
        .lines()
        .next()
        .and_then(|line| line.split_whitespace().nth(1))
        .and_then(|code| code.parse::<u16>().ok())
        .ok_or_else(|| "invalid local API status".to_string())?;

    let body = if headers
        .lines()
        .any(|line| line.eq_ignore_ascii_case("transfer-encoding: chunked"))
    {
        decode_chunked_body(body)?
    } else {
        body.to_string()
    };

    Ok(ApiFetchResponse { status, body })
}

fn decode_chunked_body(body: &str) -> Result<String, String> {
    let mut rest = body;
    let mut decoded = String::new();

    loop {
        let (size_line, after_size) = rest
            .split_once("\r\n")
            .ok_or_else(|| "invalid chunked local API response".to_string())?;
        let size_hex = size_line.split(';').next().unwrap_or("").trim();
        let size = usize::from_str_radix(size_hex, 16)
            .map_err(|err| format!("invalid chunk size: {err}"))?;
        if size == 0 {
            break;
        }
        if after_size.len() < size + 2 {
            return Err("truncated chunked local API response".to_string());
        }
        decoded.push_str(&after_size[..size]);
        rest = &after_size[size + 2..];
    }

    Ok(decoded)
}

#[tauri::command]
fn api_stream(
    app: tauri::AppHandle,
    path: String,
    body: String,
    stream_id: String,
) -> Result<(), String> {
    if !path.starts_with('/') || path.starts_with("//") || path.contains("://") {
        return Err("invalid local API path".to_string());
    }
    if stream_id.trim().is_empty() {
        return Err("missing stream id".to_string());
    }

    std::thread::spawn(move || {
        if let Err(err) = run_api_stream(&app, &path, &body, &stream_id) {
            emit_api_stream(&app, &stream_id, "error", None, None, Some(err));
        }
    });

    Ok(())
}

fn run_api_stream(
    app: &tauri::AppHandle,
    path: &str,
    body: &str,
    stream_id: &str,
) -> Result<(), String> {
    let content_length = body.len();
    let request = format!(
        "POST {path} HTTP/1.1\r\nHost: 127.0.0.1:8642\r\nContent-Type: application/json\r\nAccept: text/event-stream\r\nConnection: close\r\nContent-Length: {content_length}\r\n\r\n{body}"
    );

    let mut stream = TcpStream::connect("127.0.0.1:8642")
        .map_err(|err| format!("connect 127.0.0.1:8642 failed: {err}"))?;
    let _ = stream.set_read_timeout(Some(Duration::from_secs(300)));
    let _ = stream.set_write_timeout(Some(Duration::from_secs(10)));
    stream
        .write_all(request.as_bytes())
        .map_err(|err| format!("write local API stream request failed: {err}"))?;

    let mut pending = Vec::<u8>::new();
    let mut headers_seen = false;
    let mut chunked = false;
    let mut buf = [0_u8; 8192];

    loop {
        let n = stream
            .read(&mut buf)
            .map_err(|err| format!("read local API stream failed: {err}"))?;
        if n == 0 {
            if !pending.is_empty() {
                emit_api_stream(
                    app,
                    stream_id,
                    "chunk",
                    None,
                    Some(String::from_utf8_lossy(&pending).to_string()),
                    None,
                );
            }
            emit_api_stream(app, stream_id, "done", None, None, None);
            return Ok(());
        }

        pending.extend_from_slice(&buf[..n]);
        if !headers_seen {
            let Some(idx) = find_bytes(&pending, b"\r\n\r\n") else {
                continue;
            };
            let header_bytes = pending[..idx].to_vec();
            let body_start = pending[idx + 4..].to_vec();
            pending = body_start;

            let headers = String::from_utf8_lossy(&header_bytes);
            let status = headers
                .lines()
                .next()
                .and_then(|line| line.split_whitespace().nth(1))
                .and_then(|code| code.parse::<u16>().ok())
                .ok_or_else(|| "invalid local API stream status".to_string())?;
            chunked = headers
                .lines()
                .any(|line| line.eq_ignore_ascii_case("transfer-encoding: chunked"));
            headers_seen = true;
            emit_api_stream(app, stream_id, "status", Some(status), None, None);
        }

        if chunked {
            while let Some(chunk) = pop_chunk(&mut pending)? {
                if chunk.is_empty() {
                    emit_api_stream(app, stream_id, "done", None, None, None);
                    return Ok(());
                }
                emit_api_stream(
                    app,
                    stream_id,
                    "chunk",
                    None,
                    Some(String::from_utf8_lossy(&chunk).to_string()),
                    None,
                );
            }
        } else if !pending.is_empty() {
            let data = std::mem::take(&mut pending);
            emit_api_stream(
                app,
                stream_id,
                "chunk",
                None,
                Some(String::from_utf8_lossy(&data).to_string()),
                None,
            );
        }
    }
}

fn find_bytes(haystack: &[u8], needle: &[u8]) -> Option<usize> {
    haystack
        .windows(needle.len())
        .position(|window| window == needle)
}

fn pop_chunk(pending: &mut Vec<u8>) -> Result<Option<Vec<u8>>, String> {
    let Some(line_end) = find_bytes(pending, b"\r\n") else {
        return Ok(None);
    };
    let size_line = String::from_utf8_lossy(&pending[..line_end]);
    let size_hex = size_line.split(';').next().unwrap_or("").trim();
    let size =
        usize::from_str_radix(size_hex, 16).map_err(|err| format!("invalid chunk size: {err}"))?;
    let chunk_start = line_end + 2;
    let chunk_end = chunk_start + size;
    if pending.len() < chunk_end + 2 {
        return Ok(None);
    }
    if &pending[chunk_end..chunk_end + 2] != b"\r\n" {
        return Err("invalid chunk terminator".to_string());
    }
    let chunk = pending[chunk_start..chunk_end].to_vec();
    pending.drain(..chunk_end + 2);
    Ok(Some(chunk))
}

fn main() {
    tauri::Builder::default()
        .plugin(tauri_plugin_shell::init())
        .plugin(tauri_plugin_updater::Builder::new().build())
        .invoke_handler(tauri::generate_handler![
            api_fetch,
            api_stream,
            permissions::permissions_probe,
            permissions::permissions_request_screen_recording,
            permissions::permissions_open_settings,
        ])
        .setup(|app| {
            let parent_pid = std::process::id().to_string();

            let sidecar = app
                .shell()
                .sidecar("pan-agent")
                .expect("pan-agent sidecar missing from bundle")
                .args(["serve", "--host", "127.0.0.1", "--port", "8642"])
                .env("PAN_AGENT_PARENT_PID", &parent_pid);

            let (mut rx, child) = sidecar.spawn().expect("failed to spawn pan-agent sidecar");

            app.manage(SidecarHandle(Mutex::new(Some(child))));

            tauri::async_runtime::spawn(async move {
                while let Some(event) = rx.recv().await {
                    match event {
                        CommandEvent::Stdout(line) => {
                            eprintln!("[pan-agent] {}", String::from_utf8_lossy(&line).trim_end());
                        }
                        CommandEvent::Stderr(line) => {
                            eprintln!("[pan-agent] {}", String::from_utf8_lossy(&line).trim_end());
                        }
                        CommandEvent::Terminated(payload) => {
                            eprintln!(
                                "[pan-agent] sidecar terminated: code={:?} signal={:?}",
                                payload.code, payload.signal
                            );
                        }
                        _ => {}
                    }
                }
            });

            Ok(())
        })
        .build(tauri::generate_context!())
        .expect("error while building pan-desktop")
        .run(|app_handle, event| {
            if let RunEvent::ExitRequested { .. } = event {
                if let Some(state) = app_handle.try_state::<SidecarHandle>() {
                    if let Some(child) = state.0.lock().unwrap().take() {
                        let _ = child.kill();
                    }
                }
            }
        });
}
