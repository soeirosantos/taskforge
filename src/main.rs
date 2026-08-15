use axum::{routing::get, Json, Router};
use serde_json::{json, Value};

/// Builds the application router. Contains no I/O and no environment access.
fn app() -> Router {
    Router::new().route("/health", get(health))
}

async fn health() -> Json<Value> {
    Json(json!({ "status": "ok" }))
}

/// Resolves the port to bind from the raw `PORT` environment variable value.
///
/// An unset value (`None`) resolves to the default port 8080. Any other
/// value must parse as a `u16` and must not be `0`.
fn resolve_port(raw: Option<&str>) -> Result<u16, String> {
    let raw = match raw {
        None => return Ok(8080),
        Some(raw) => raw,
    };

    if raw.trim().is_empty() {
        return Err(format!(
            "invalid PORT value {raw:?}: expected a number between 1 and 65535, got an empty value"
        ));
    }

    match raw.trim().parse::<u16>() {
        Ok(0) => Err(format!(
            "invalid PORT value {raw:?}: expected a number between 1 and 65535, got 0"
        )),
        Ok(port) => Ok(port),
        Err(_) => Err(format!(
            "invalid PORT value {raw:?}: expected a number between 1 and 65535"
        )),
    }
}

#[tokio::main]
async fn main() {
    let raw_port = std::env::var("PORT").ok();
    let port = match resolve_port(raw_port.as_deref()) {
        Ok(port) => port,
        Err(message) => {
            eprintln!("{message}");
            std::process::exit(1);
        }
    };

    println!("server starting on port {port}");

    let listener = tokio::net::TcpListener::bind(("0.0.0.0", port))
        .await
        .expect("failed to bind TCP listener");
    axum::serve(listener, app()).await.expect("server error");
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::body::{to_bytes, Body};
    use axum::http::{Request, StatusCode};
    use tower::ServiceExt;

    #[tokio::test]
    async fn health_get_returns_200() {
        let response = app()
            .oneshot(
                Request::builder()
                    .uri("/health")
                    .body(Body::empty())
                    .unwrap(),
            )
            .await
            .unwrap();

        assert_eq!(response.status(), StatusCode::OK);
    }

    #[tokio::test]
    async fn health_get_returns_json_content_type() {
        let response = app()
            .oneshot(
                Request::builder()
                    .uri("/health")
                    .body(Body::empty())
                    .unwrap(),
            )
            .await
            .unwrap();

        let content_type = response
            .headers()
            .get(axum::http::header::CONTENT_TYPE)
            .expect("missing Content-Type header")
            .to_str()
            .expect("Content-Type is not valid UTF-8");

        assert!(
            content_type.starts_with("application/json"),
            "expected a JSON content type, got {content_type:?}"
        );
    }

    #[tokio::test]
    async fn health_get_body_is_status_ok_json() {
        let response = app()
            .oneshot(
                Request::builder()
                    .uri("/health")
                    .body(Body::empty())
                    .unwrap(),
            )
            .await
            .unwrap();

        let body_bytes = to_bytes(response.into_body(), usize::MAX).await.unwrap();
        let body_json: Value = serde_json::from_slice(&body_bytes).unwrap();

        assert_eq!(body_json, json!({ "status": "ok" }));
    }

    #[tokio::test]
    async fn health_post_returns_405() {
        let response = app()
            .oneshot(
                Request::builder()
                    .method("POST")
                    .uri("/health")
                    .body(Body::empty())
                    .unwrap(),
            )
            .await
            .unwrap();

        assert_eq!(response.status(), StatusCode::METHOD_NOT_ALLOWED);
    }

    #[tokio::test]
    async fn unknown_route_returns_404() {
        let response = app()
            .oneshot(
                Request::builder()
                    .uri("/unknown")
                    .body(Body::empty())
                    .unwrap(),
            )
            .await
            .unwrap();

        assert_eq!(response.status(), StatusCode::NOT_FOUND);
    }

    #[tokio::test]
    async fn health_head_returns_200() {
        let response = app()
            .oneshot(
                Request::builder()
                    .method("HEAD")
                    .uri("/health")
                    .body(Body::empty())
                    .unwrap(),
            )
            .await
            .unwrap();

        assert_eq!(response.status(), StatusCode::OK);
    }

    #[test]
    fn resolve_port_defaults_to_8080_when_unset() {
        assert_eq!(resolve_port(None), Ok(8080));
    }

    #[test]
    fn resolve_port_accepts_valid_numeric_value() {
        assert_eq!(resolve_port(Some("9000")), Ok(9000));
    }

    #[test]
    fn resolve_port_rejects_non_numeric_value() {
        assert!(resolve_port(Some("abc")).is_err());
    }

    #[test]
    fn resolve_port_rejects_out_of_range_value() {
        assert!(resolve_port(Some("70000")).is_err());
    }

    #[test]
    fn resolve_port_rejects_zero() {
        assert!(resolve_port(Some("0")).is_err());
    }

    #[test]
    fn resolve_port_rejects_empty_value() {
        assert!(resolve_port(Some("")).is_err());
    }
}
