# jarvis

A local voice assistant for NixOS. You speak, whisper transcribes it on the
CPU, an LLM in [llama-swap](https://github.com/mostlygeek/llama-swap) answers
as a character, and Qwen3-TTS speaks the answer in that character's voice.
Nothing leaves the machine except the web searches you ask for.

NixOS only. The TTS runs in a ROCm container, so the voices need an AMD GPU.

## What's in here

| Path | What |
|---|---|
| `module.nix` | `services.jarvis`: the user unit, `/etc/jarvis`, the models, the TTS containers |
| `package.nix`, `daemon/` | the Go daemon and the `jarvis` CLI |
| `characters/<id>/` | `character.toml`, `persona.md`, `ref.txt` for build-time clips |
| `prompts/base.md` | the rules every character follows |
| `models.nix`, `models.lock.json` | every downloaded file, by URL and sha256 |
| `update-models` | fills `models.lock.json` from the Hugging Face API |
| `refs.nix` | the reference clips the cloned voices are made from |

The menu is not here: it belongs to your desktop. Any client that speaks the
socket protocol below works. The author's is a Quickshell menu in
[Luvrok/dotfiles](https://github.com/Luvrok/dotfiles)
(`nixos/services/hyprland/quickshell/menus/Jarvis.qml`).

## Use it

```nix
# flake.nix
inputs.jarvis.url = "git+https://github.com/Luvrok/jarvis?shallow=1";
inputs.jarvis.inputs.nixpkgs.follows = "nixpkgs";

# a host's modules
inputs.jarvis.nixosModules.default
{
  services.jarvis.enable = true;
  # optional
  services.jarvis.proxy = "http://127.0.0.1:10808";
  services.jarvis.tools.notesMcp = "http://127.0.0.1:6281/mcp";
}
```

Then add the models to llama-swap's config (below) and build. The first
build downloads about 6 GB of models. Before the voices work, build their
container once (ROCm PyTorch, about 10 GB):

```bash
sudo systemctl start jarvis-tts-image
```

```bash
jarvis check
```

## llama-swap

The module doesn't own llama-swap's config. Add these entries to yours. The
LLM is any chat model, `jarvis` is its name for the daemon; the TTS entries
keep their ports, the module's units listen there.

```yaml
models:
  "jarvis":
    cmd: |
      llama-server --port ${PORT} -np 1
      -m /var/lib/models/gemma-4-12B-it-qat-UD-Q4_K_XL.gguf
      --ctx-size 8192 -fa on --cache-type-k q8_0 --cache-type-v q8_0
      --lazy-mode on --cache-ram 0
    ttl: 600

  "jarvis-tts-base":
    cmd: jarvis-tts base
    cmdStop: systemctl stop jarvis-tts-base.service
    proxy: http://127.0.0.1:8881
    checkEndpoint: /health
    useModelName: qwen3-tts
    ttl: 600

  # only after ./update-models tts-base-small
  "jarvis-tts-small":
    cmd: jarvis-tts small
    cmdStop: systemctl stop jarvis-tts-small.service
    proxy: http://127.0.0.1:8883
    checkEndpoint: /health
    useModelName: qwen3-tts
    ttl: 600

  "jarvis-tts-kurisu":
    cmd: jarvis-tts kurisu
    cmdStop: systemctl stop jarvis-tts-kurisu.service
    proxy: http://127.0.0.1:8882
    checkEndpoint: /health
    useModelName: qwen3-tts
    ttl: 600

routing:
  router:
    use: group
    settings:
      groups:
        # one voice at a time, next to the llm (put jarvis in your llm group)
        "jarvis-tts":
          swap: true
          exclusive: false
          members: [jarvis-tts-base, jarvis-tts-small, jarvis-tts-kurisu]
```

`--cache-ram 0` matters: llama-server keeps up to 8 GiB of old prompts in RAM
by default, and jarvis' prompt changes every minute (it has the time in it).
`--lazy-mode on` keeps Gemma 4's per-layer embeddings on disk instead of RAM.

## Options

| Option | Default | |
|---|---|---|
| `enable` | `false` | |
| `character` | `"jarvis"` | the one at the start, the menu's pick wins after |
| `wakeWord.enable` | `true` | the active character's "hey ..." |
| `audio.source` | `"default"` | the input device, the menu's pick wins after |
| `history.turns` | `6` | exchanges kept for the next question |
| `notify` | `false` | the answer as a notification while no menu is open |
| `llamaSwapUrl` | `http://127.0.0.1:11434` | |
| `tools.webSearch` | the local searxng, if on | `null` turns web search off |
| `tools.notesMcp` | `null` | a docs-mcp-server with a `notes` library |
| `openCmd` | `qs -c hyprland ipc call menu open jarvis` | what the wake word runs |
| `path` | `[ quickshell ]` | what `openCmd` needs |
| `proxy` | `null` | for the Hugging Face files and the image build only |
| `modelsDir` | `/var/lib/models/jarvis` | |
| `ui.qmlFiles` | `[ ]` | QML checked at build time: no `radius`, square shapes |
| `package` | this flake's | |

## The CLI

```
jarvis listen            listen now, print the transcript and the answer
jarvis stop              stop listening or speaking
jarvis pause | resume    close and open the mic, wake word too
jarvis new               a new chat
jarvis unload            free the vram, and a new chat
jarvis clones small|big  the cloned voices on the 0.6b or the 1.7b model
jarvis last              the last answer
jarvis check             what keeps jarvis from working
```

## Characters

A folder in `characters/` with a `character.toml` and a `persona.md`:

```toml
name = "Makise Kurisu"
wake = "hey kurisu"
lang = "auto"          # auto, en or ru
icon = "󰙍"

[voice]
model = "jarvis-tts-kurisu"   # a custom-voice checkpoint and its speaker
speaker = "kurisu"
# or a clone on the base model, of refs/<id>/ref.wav and ref.txt:
# model = "jarvis-tts-base"
# ref = "ref.wav"
# ref_text = "ref.txt"
```

A cloned voice needs its clip in `refs.nix`, and the files it comes from in
`models.nix` / `models.lock.json`.

The fan-made character voices are for personal use only.

## The socket

`$XDG_RUNTIME_DIR/jarvis.sock`, one JSON object per line, each with a
`type`.

The daemon sends `hello`, `state`, `level` (0..1, `mic` or `tts`, ~30 Hz),
`transcript`, `answer`, `characters`, `sources`, `loaded` (its models
llama-swap has up), `clones`, `new_chat`, `error`, `check`.

A client sends `listen`, `stop`, `close`, `pause`, `resume`, `set_source`,
`set_character`, `preview_character`, `set_clones`, `new_chat`, `unload`,
`last_answer`, `check`, and `watch` for a passive client (a bar icon): while
only passive clients are connected, the answer reopens the menu.

States: `idle`, `listening`, `transcribing`, `thinking`, `searching`,
`voicing`, `speaking`, `paused`, `error`.

## Models

`./update-models` fills `models.lock.json` from the Hugging Face API: each
repo's commit and a sha256 per file (LFS files carry it there, nothing big
is downloaded). It keeps every entry at its locked commit, so a run without
changes changes nothing; `--latest` moves entries to their repo's main:

```bash
./update-models --latest            # all of them
./update-models --latest tts-base   # one
```

A new entry in the lock starts with `"rev": "main"` and `"files": {}`.
