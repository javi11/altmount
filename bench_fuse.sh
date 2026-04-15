#!/usr/bin/env bash
# ============================================================
#  AltMount Media Streaming Benchmark — macOS
#  Compares FUSE mount vs rclone mount side-by-side
# ============================================================

set -euo pipefail

# ── Usage ────────────────────────────────────────────────────
usage() {
  echo "Usage: $0 <fuse_file> <rclone_file> [read_mb]"
  echo ""
  echo "  fuse_file    Path to file on the FUSE mount"
  echo "  rclone_file  Path to same file on the rclone mount"
  echo "  read_mb      MB to read in sequential test (default: 256)"
  exit 1
}

FUSE_FILE="${1:-}"
RCLONE_FILE="${2:-}"
READ_MB="${3:-256}"

[ -z "$FUSE_FILE" ] || [ -z "$RCLONE_FILE" ] && usage

GREEN='\033[0;32m'; CYAN='\033[0;36m'; YELLOW='\033[1;33m'
RED='\033[0;31m'; BOLD='\033[1m'; NC='\033[0m'

hr()   { echo -e "${CYAN}────────────────────────────────────────────────${NC}"; }
hdr()  { hr; echo -e "${BOLD}${YELLOW}  $1${NC}"; hr; }
log()  { echo -e "  ${CYAN}[→]${NC} $1"; }
ok()   { echo -e "  ${GREEN}[✓]${NC} $1"; }
warn() { echo -e "  ${YELLOW}[!]${NC} $1"; }
err()  { echo -e "  ${RED}[✗]${NC} $1"; }

now_ms() {
  python3 -c "import time; print(int(time.time()*1000))"
}

# ── Results storage (plain variables, bash 3.2 compatible) ───
FUSE_TTFB_AVG="" FUSE_SEQ_AVG_SPEED="" FUSE_SEEK_AVG="" FUSE_CHUNK_SPEEDS=""
RCLONE_TTFB_AVG="" RCLONE_SEQ_AVG_SPEED="" RCLONE_SEEK_AVG="" RCLONE_CHUNK_SPEEDS=""

run_tests() {
  local file="$1"
  local label="$2"  # "FUSE" or "rclone"
  local file_size
  file_size=$(stat -f%z "$file")

  # ── Test 1: Time-to-First-Byte ──────────────────────────────
  hdr "[$label] Test 1 — Time-to-First-Byte"
  echo -e "  Reads first 512KB — simulates Emby opening file before playback"
  echo ""

  local ttfb_total=0 ttfb_runs="" i start end elapsed
  for i in 1 2 3; do
    log "Run $i/3 — reading first 512KB..."
    start=$(now_ms)
    dd if="$file" of=/dev/null bs=512k count=1 2>/dev/null \
      && ok "dd completed" || { err "dd failed on TTFB run $i"; exit 1; }
    end=$(now_ms)
    elapsed=$(( end - start ))
    echo -e "  ${BOLD}Run $i: ${GREEN}${elapsed}ms${NC}"
    ttfb_runs="$ttfb_runs $elapsed"
    ttfb_total=$(( ttfb_total + elapsed ))
    echo ""
  done

  local ttfb_avg=$(( ttfb_total / 3 ))
  echo -e "  ${BOLD}Average TTFB: ${GREEN}${ttfb_avg}ms${NC}"
  if   [ "$ttfb_avg" -lt 500  ]; then ok "Excellent — playback starts near-instantly"
  elif [ "$ttfb_avg" -lt 1500 ]; then warn "Acceptable — slight delay before playback"
  else                                 err "Slow — noticeable buffering before playback starts"
  fi

  [ "$label" = "FUSE" ] && FUSE_TTFB_AVG="$ttfb_avg" || RCLONE_TTFB_AVG="$ttfb_avg"

  # ── Test 2: Sequential Read Speed ───────────────────────────
  hdr "[$label] Test 2 — Sequential Read Speed"
  echo -e "  Reads ${READ_MB}MB — simulates Emby read-ahead buffer"
  echo ""

  local seq_total=0 seq_runs="" speed
  for i in 1 2 3; do
    log "Run $i/3 — reading ${READ_MB}MB sequentially..."
    start=$(now_ms)
    dd if="$file" of=/dev/null bs=4m count=$(( READ_MB / 4 )) 2>/dev/null \
      && ok "dd completed" || { err "dd failed on Sequential run $i"; exit 1; }
    end=$(now_ms)
    elapsed=$(( end - start ))
    speed=$(echo "scale=1; $READ_MB * 1000 / $elapsed" | bc)
    echo -e "  ${BOLD}Run $i: ${GREEN}${speed} MB/s${NC} (${elapsed}ms)"
    seq_runs="$seq_runs $speed"
    seq_total=$(( seq_total + elapsed ))
    echo ""
  done

  local seq_avg_ms=$(( seq_total / 3 ))
  local seq_avg_speed
  seq_avg_speed=$(echo "scale=1; $READ_MB * 1000 / $seq_avg_ms" | bc)
  echo -e "  ${BOLD}Average: ${GREEN}${seq_avg_speed} MB/s${NC}"
  local speed_int=${seq_avg_speed%.*}
  if   [ "$speed_int" -ge 80 ]; then ok "Excellent — handles 4K HDR with headroom"
  elif [ "$speed_int" -ge 30 ]; then ok "Good — handles 1080p and most 4K comfortably"
  elif [ "$speed_int" -ge 10 ]; then warn "Marginal — fine for 1080p, may buffer on heavy 4K"
  else                                err "Too slow — expect buffering even on 1080p"
  fi

  [ "$label" = "FUSE" ] && FUSE_SEQ_AVG_SPEED="$seq_avg_speed" || RCLONE_SEQ_AVG_SPEED="$seq_avg_speed"

  # ── Test 3: Seek Performance ─────────────────────────────────
  hdr "[$label] Test 3 — Seek + Read"
  echo -e "  Jumps to 25%/50%/75% of file and reads 32MB from each"
  echo ""

  local seek_total=0 seek_runs="" offset skip_blocks offset_gb pct
  local percents=(25 50 75)

  for i in 0 1 2; do
    pct="${percents[$i]}"
    offset=$(echo "$file_size * $pct / 100" | bc)
    skip_blocks=$(echo "$offset / 4194304" | bc)
    offset_gb=$(echo "scale=1; $offset / 1073741824" | bc)

    log "Jumping to ${pct}% (~${offset_gb}GB into file), reading 32MB..."
    start=$(now_ms)
    dd if="$file" of=/dev/null bs=4m count=8 skip="$skip_blocks" 2>/dev/null \
      && ok "dd completed" || { err "dd failed at seek ${pct}%"; exit 1; }
    end=$(now_ms)
    elapsed=$(( end - start ))
    echo -e "  ${BOLD}Jump to ${pct}% (~${offset_gb}GB): ${GREEN}${elapsed}ms${NC}"
    seek_runs="$seek_runs $elapsed"
    seek_total=$(( seek_total + elapsed ))
    echo ""
  done

  local seek_avg=$(( seek_total / 3 ))
  echo -e "  ${BOLD}Average seek time: ${GREEN}${seek_avg}ms${NC}"
  if   [ "$seek_avg" -lt 800  ]; then ok "Excellent — scrubbing will feel responsive"
  elif [ "$seek_avg" -lt 2000 ]; then warn "Acceptable — short pause when scrubbing"
  else                                 err "Slow — expect buffering spinner when jumping"
  fi

  [ "$label" = "FUSE" ] && FUSE_SEEK_AVG="$seek_avg" || RCLONE_SEEK_AVG="$seek_avg"

  # ── Test 4: Sustained Stability ──────────────────────────────
  hdr "[$label] Test 4 — Sustained Stability"
  echo -e "  6 consecutive 32MB chunks — mirrors real uninterrupted playback"
  echo ""

  local chunk_speeds="" skip
  for i in 1 2 3 4 5 6; do
    skip=$(( (i - 1) * 8 ))
    log "Chunk $i/6: reading 32MB at skip=${skip} blocks..."
    start=$(now_ms)
    dd if="$file" of=/dev/null bs=4m count=8 skip="$skip" 2>/dev/null \
      && ok "dd completed" || { err "dd failed on chunk $i"; exit 1; }
    end=$(now_ms)
    elapsed=$(( end - start ))
    speed=$(echo "scale=1; 32 * 1000 / $elapsed" | bc)
    echo -e "  ${BOLD}Chunk $i: ${GREEN}${speed} MB/s${NC} (${elapsed}ms)"
    chunk_speeds="$chunk_speeds $speed"
    echo ""
  done

  echo -e "  ${BOLD}All chunk speeds: ${GREEN}${chunk_speeds}${NC}"
  [ "$label" = "FUSE" ] && FUSE_CHUNK_SPEEDS="$chunk_speeds" || RCLONE_CHUNK_SPEEDS="$chunk_speeds"
}

# ─── Step 1: Verify files ────────────────────────────────────
hdr "Step 1 — Verifying test files"

for entry in "FUSE:$FUSE_FILE" "rclone:$RCLONE_FILE"; do
  label="${entry%%:*}"
  file="${entry#*:}"
  log "Checking $label file..."
  if [ ! -f "$file" ]; then
    err "$label file not found: $file"
    exit 1
  fi
  size=$(stat -f%z "$file")
  size_gb=$(echo "scale=2; $size / 1073741824" | bc)
  ok "$label: $file (${size_gb} GB)"
done
echo ""

# ─── Step 2: Run FUSE tests ──────────────────────────────────
hdr "Running FUSE mount tests"
run_tests "$FUSE_FILE" "FUSE"

# ─── Step 3: Run rclone tests ────────────────────────────────
hdr "Running rclone mount tests"
run_tests "$RCLONE_FILE" "rclone"

# ─── Final Comparison Summary ────────────────────────────────
hdr "Final Comparison Summary"

fuse_size=$(stat -f%z "$FUSE_FILE")
fuse_size_gb=$(echo "scale=2; $fuse_size / 1073741824" | bc)

echo ""
printf "  ${BOLD}%-30s %-20s %-20s${NC}\n" "Metric" "FUSE" "rclone"
hr
printf "  %-30s ${GREEN}%-20s${NC} ${CYAN}%-20s${NC}\n" \
  "Playback start (TTFB)" \
  "${FUSE_TTFB_AVG}ms avg" \
  "${RCLONE_TTFB_AVG}ms avg"
printf "  %-30s ${GREEN}%-20s${NC} ${CYAN}%-20s${NC}\n" \
  "Sequential speed" \
  "${FUSE_SEQ_AVG_SPEED} MB/s avg" \
  "${RCLONE_SEQ_AVG_SPEED} MB/s avg"
printf "  %-30s ${GREEN}%-20s${NC} ${CYAN}%-20s${NC}\n" \
  "Seek performance" \
  "${FUSE_SEEK_AVG}ms avg" \
  "${RCLONE_SEEK_AVG}ms avg"
printf "  %-30s ${GREEN}%-20s${NC} ${CYAN}%-20s${NC}\n" \
  "Sustained chunks" \
  "see runs below" \
  "see runs below"
hr
echo ""
echo -e "  ${BOLD}FUSE chunk speeds:   ${GREEN}${FUSE_CHUNK_SPEEDS}${NC} MB/s"
echo -e "  ${BOLD}rclone chunk speeds: ${CYAN}${RCLONE_CHUNK_SPEEDS}${NC} MB/s"
echo ""
echo -e "${YELLOW}  4K HDR reference targets (worst case):${NC}"
echo -e "     TTFB        < 500ms    = instant playback start"
echo -e "     Sequential  > 80 MB/s  = handles 4K HDR REMUX comfortably"
echo -e "                 > 30 MB/s  = handles 1080p safely"
echo -e "     Seek        < 800ms    = responsive scrubbing"
echo -e "     Sustained   stable     = no mid-movie buffering"
echo ""
