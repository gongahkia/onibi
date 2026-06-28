#!/usr/bin/env sh
set -eu

PREFIX="${KELP_LLAMA_PREFIX:-.kelp-pi/llama/host}"
MODEL="${KELP_PI_MODEL:-.kelp-pi/models/Qwen_Qwen3-0.6B-Q4_K_M.gguf}"
PROMPT="${KELP_LLAMA_SMOKE_PROMPT:-<|im_start|>user
/no_think Reply with exactly: ready<|im_end|>
<|im_start|>assistant
}"

if [ ! -f "$MODEL" ]; then
  echo "missing model $MODEL" >&2
  exit 66
fi

scripts/build-llama.sh >/dev/null
zig build -Dllama=true -Dllama-prefix="$PREFIX"

OUTPUT="$(
  ./zig-out/bin/kelp-pi model prompt \
    --data-dir .kelp-pi \
    --id qwen3-0.6b-q4_k_m \
    --model-path "$MODEL" \
    --prompt "$PROMPT" \
    --n-predict "${KELP_LLAMA_SMOKE_TOKENS:-12}" \
    --threads "${KELP_LLAMA_THREADS:-2}"
)"

printf '%s\n' "$OUTPUT"
printf '%s\n' "$OUTPUT" | node -e '
let s = "";
process.stdin.on("data", d => s += d);
process.stdin.on("end", () => {
  const payload = JSON.parse(s);
  if (!payload.ok || !payload.loaded || payload.decodedTokens < 1) process.exit(1);
});
'
