#!/bin/bash
# OpenFusion — multi-model fusion engine
# Location: ~/ws/apps/openfusion/run.sh
# Port: 8080 | Config: config.yaml

set -e

APP_DIR="$(cd "$(dirname "$0")" && pwd)"
BINARY="$APP_DIR/openfusion"
CONFIG="$APP_DIR/config.yaml"
PID_FILE="$APP_DIR/.pid"
LOG_FILE="$APP_DIR/openfusion.log"
PORT=8080

running() {
  if [ -f "$PID_FILE" ]; then
    local pid=$(cat "$PID_FILE")
    if kill -0 "$pid" 2>/dev/null; then
      return 0
    fi
  fi
  lsof -tiTCP:$PORT -sTCP:LISTEN &>/dev/null && return 0
  return 1
}

start() {
  if running; then
    echo "✅ OpenFusion already running on port $PORT"
    return 0
  fi
  echo "🚀 Starting OpenFusion..."
  cd "$APP_DIR"
  nohup "$BINARY" -config "$CONFIG" > "$LOG_FILE" 2>&1 &
  echo $! > "$PID_FILE"
  sleep 2
  if running; then
    echo "✅ Started (PID $(cat "$PID_FILE"), port $PORT)"
    curl -s "http://127.0.0.1:$PORT/v1/models" | python3 -m json.tool 2>/dev/null | head -10
  else
    echo "❌ Failed — check $LOG_FILE"
    tail -10 "$LOG_FILE"
    return 1
  fi
}

stop() {
  if ! running; then
    echo "⚠️  OpenFusion not running"
    rm -f "$PID_FILE"
    return 0
  fi
  echo "🛑 Stopping OpenFusion..."
  local pid
  if [ -f "$PID_FILE" ]; then
    pid=$(cat "$PID_FILE")
    kill "$pid" 2>/dev/null && echo "✅ Stopped (PID $pid)"
  fi
  lsof -tiTCP:$PORT -sTCP:LISTEN 2>/dev/null | xargs kill 2>/dev/null || true
  rm -f "$PID_FILE"
}

status() {
  if running; then
    echo "✅ Running on port $PORT"
    curl -s "http://127.0.0.1:$PORT/v1/models" 2>/dev/null | python3 -c "import sys,json; d=json.load(sys.stdin); print('Models:', len(d.get('data',d) if isinstance(d,dict) else d))" 2>/dev/null
    echo "---"
    curl -s "http://127.0.0.1:$PORT/v1/dashboard" 2>/dev/null | head -5
  else
    echo "⏹️  Not running"
  fi
}

restart() { stop; sleep 1; start; }

case "${1:-start}" in
  start)   start ;;
  stop)    stop ;;
  restart) restart ;;
  status)  status ;;
  *)       echo "Usage: $0 {start|stop|restart|status}" ;;
esac
