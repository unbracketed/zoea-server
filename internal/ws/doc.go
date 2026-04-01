// Package ws will host websocket fanout infrastructure for session streams.
//
// Planned responsibilities:
//   - per-session subscriber registry
//   - bounded outbound buffers/backpressure policy
//   - optional inbound command forwarding (e.g., extension_ui_response)
package ws
