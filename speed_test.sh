#!/bin/bash
# Speed test: Internal FUSE mount vs rclone mount
# Reads 500MB chunks via dd, uses dd's own reported throughput

FUSE_FILE="${1:?Usage: $0 <fuse_file> <rclone_file> [runs] [read_mb]}"
RCLONE_FILE="${2:?Usage: $0 <fuse_file> <rclone_file> [runs] [read_mb]}"
RUNS="${3:-3}"
READ_MB="${4:-2000}"

echo "=========================================="
echo "  Mount Speed Test"
echo "=========================================="
echo ""

# Check files exist
for label_file in "FUSE:$FUSE_FILE" "RCLONE:$RCLONE_FILE"; do
  label="${label_file%%:*}"
  file="${label_file#*:}"
  if [ ! -f "$file" ]; then
    echo "ERROR: $label file not found: $file"
    exit 1
  fi
done

FILE_SIZE=$(stat -f%z "$FUSE_FILE" 2>/dev/null || stat -c%s "$FUSE_FILE" 2>/dev/null)
FILE_SIZE_MB=$((FILE_SIZE / 1048576))
echo "File size: ${FILE_SIZE_MB} MB"
echo "Read size per run: ${READ_MB} MB"
echo "Runs per mount: ${RUNS}"
echo ""

purge_cache() {
  sync
  sudo purge 2>/dev/null || true
}

run_test() {
  local label="$1"
  local file="$2"
  local total_speed=0
  local total_time=0

  echo "------------------------------------------"
  echo "  Testing: $label"
  echo "------------------------------------------"

  for i in $(seq 1 $RUNS); do
    purge_cache
    sleep 2

    # Read from start each run (full 2GB read)
    local skip_mb=0

    # Capture dd stderr for its timing info, use wall clock for our own measurement
    start=$(python3 -c 'import time; print(f"{time.monotonic():.6f}")')
    dd_output=$(dd if="$file" of=/dev/null bs=1m count=$READ_MB skip=$skip_mb 2>&1)
    end=$(python3 -c 'import time; print(f"{time.monotonic():.6f}")')

    elapsed=$(echo "$end - $start" | bc)
    if [ "$(echo "$elapsed < 0.01" | bc)" -eq 1 ]; then
      elapsed="0.01"
    fi
    speed=$(echo "scale=2; $READ_MB / $elapsed" | bc)
    total_time=$(echo "$total_time + $elapsed" | bc)
    total_speed=$(echo "$total_speed + $speed" | bc)

    # Also show dd's own report
    dd_speed=$(echo "$dd_output" | grep -oE '[0-9.]+ (bytes|MB|GB)/sec' | head -1)

    printf "  Run %d (skip %dMB): %.3fs  %.2f MB/s  [dd: %s]\n" \
      "$i" "$skip_mb" "$elapsed" "$speed" "${dd_speed:-n/a}"
  done

  avg_time=$(echo "scale=3; $total_time / $RUNS" | bc)
  avg_speed=$(echo "scale=2; $total_speed / $RUNS" | bc)
  printf "  Average: %.3fs  (%.2f MB/s)\n\n" "$avg_time" "$avg_speed"

  eval "${label}_AVG_SPEED=$avg_speed"
  eval "${label}_AVG_TIME=$avg_time"
}

echo ">>> Testing FUSE mount..."
purge_cache
sleep 3
run_test "FUSE" "$FUSE_FILE"

echo ">>> Testing RCLONE mount..."
purge_cache
sleep 3
run_test "RCLONE" "$RCLONE_FILE"

echo "=========================================="
echo "  Results Summary"
echo "=========================================="
printf "  FUSE (internal):  %s MB/s  (avg %ss)\n" "$FUSE_AVG_SPEED" "$FUSE_AVG_TIME"
printf "  RCLONE mount:     %s MB/s  (avg %ss)\n" "$RCLONE_AVG_SPEED" "$RCLONE_AVG_TIME"

if [ "$(echo "${FUSE_AVG_SPEED:-0} > ${RCLONE_AVG_SPEED:-0}" | bc)" -eq 1 ]; then
  diff=$(echo "scale=1; ($FUSE_AVG_SPEED / $RCLONE_AVG_SPEED - 1) * 100" | bc)
  echo "  --> FUSE is ${diff}% faster"
elif [ "$(echo "${RCLONE_AVG_SPEED:-0} > ${FUSE_AVG_SPEED:-0}" | bc)" -eq 1 ]; then
  diff=$(echo "scale=1; ($RCLONE_AVG_SPEED / $FUSE_AVG_SPEED - 1) * 100" | bc)
  echo "  --> RCLONE is ${diff}% faster"
else
  echo "  --> Both are equal"
fi
echo "=========================================="
