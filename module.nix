# jarvis: a local voice assistant. the go daemon listens (wake word, vad
# and speech to text on the cpu), an llm in llama-swap answers as the active
# character, qwen3-tts in llama-swap speaks it in that character's voice.
# a menu talks to the daemon over its socket, that lives with the desktop
# (see the readme). off by default. off means nothing here: no unit, no
# config, no models
{
  config,
  lib,
  pkgs,
  ...
}:

let
  cfg = config.services.jarvis;
  inherit (cfg) proxy modelsDir;
  models = import ./models.nix { inherit pkgs lib proxy; };

  # the speech to text models: what the menu shows, their kind, the files
  sttModels = {
    parakeet-v3 = {
      name = "Parakeet v3 · ru en";
      kind = "transducer";
      files = models.parakeet-v3;
    };
    gigaam-v2 = {
      name = "GigaAM v2 · ru";
      kind = "transducer";
      files = models.gigaam-v2;
    };
    whisper-small = {
      name = "Whisper small";
      kind = "whisper";
      files = models.whisper-small;
    };
  };
  refs = import ./refs.nix { inherit pkgs lib models; };
  podman = lib.getExe config.virtualisation.podman.package;
  jarvis = cfg.package;

  characterDirs = lib.attrNames (
    lib.filterAttrs (_: t: t == "directory") (builtins.readDir ./characters)
  );
  characterOf = id: builtins.fromTOML (builtins.readFile (./characters + "/${id}/character.toml"));
  cloned = lib.filter (id: (characterOf id).voice.speaker or "" == "") characterDirs;

  # the characters, each with wake.txt: its wake phrase in the kws model's
  # bpe tokens, "▁HE Y ▁J AR VI S @HEY_JARVIS"
  characters =
    pkgs.runCommandLocal "jarvis-characters"
      { nativeBuildInputs = [ (pkgs.python3.withPackages (p: [ p.sentencepiece ])) ]; }
      ''
        cp -r ${./characters} $out
        chmod -R u+w $out
        python3 - $out ${models.kws}/bpe.model <<'EOF'
        import pathlib, sys, tomllib
        import sentencepiece as spm
        sp = spm.SentencePieceProcessor(model_file=sys.argv[2])
        for d in sorted(pathlib.Path(sys.argv[1]).iterdir()):
            wake = tomllib.loads((d / "character.toml").read_text()).get("wake", "").strip().upper()
            if wake:
                tokens = " ".join(sp.encode(wake, out_type=str))
                (d / "wake.txt").write_text(f"{tokens} @{wake.replace(' ', '_')}\n")
        EOF
      '';

  configFile = (pkgs.formats.toml { }).generate "jarvis.toml" {
    llm = {
      url = cfg.llamaSwapUrl;
      model = cfg.llm;
      max_tokens = 200;
      choices = lib.mapAttrsToList (model: name: { inherit model name; }) cfg.llms;
    };
    tts = {
      url = cfg.llamaSwapUrl;
      # the clones' quicker model, "" while it isn't downloaded
      small_model = if models.tts-base-small != null then "jarvis-tts-small" else "";
    };
    character = {
      default = cfg.character;
      dir = "/etc/jarvis/characters";
      base_prompt = "/etc/jarvis/prompts/base.md";
      refs = "${modelsDir}/refs";
    };
    stt = {
      default = cfg.stt.model;
      choices = map (id: {
        inherit id;
        inherit (sttModels.${id}) name kind;
        dir = "${modelsDir}/stt/${id}";
      }) cfg.stt.models;
      threads = 4;
    };
    vad.model = "${modelsDir}/silero_vad.onnx";
    wake_word = {
      inherit (cfg.wakeWord) enable;
      model = "${modelsDir}/kws";
    };
    audio = {
      inherit (cfg.audio) source;
      silence_ms = 1000;
      max_record_s = 30;
    };
    history = {
      inherit (cfg.history) turns;
      forget_after_s = 600;
    };
    tools = {
      web_search = if cfg.tools.webSearch == null then "" else cfg.tools.webSearch;
      notes_mcp = if cfg.tools.notesMcp == null then "" else cfg.tools.notesMcp;
      notes_library = "notes";
      inherit (cfg.tools) apps;
      terminal = if cfg.tools.terminal == null then "" else cfg.tools.terminal;
    };
    ui = {
      open_cmd = cfg.openCmd;
      inherit (cfg) notify;
      ready_sound = "/etc/jarvis/sounds/ready.ogg";
    };
  };

  # the tts server's voice library: a profile per cloned character, the
  # daemon asks for voice "clone:<id>"
  voices = pkgs.linkFarm "jarvis-voices" (
    lib.concatMap (id: [
      {
        name = "profiles/${id}/meta.json";
        path = pkgs.runCommandLocal "jarvis-voice-${id}.json" { nativeBuildInputs = [ pkgs.jq ]; } ''
          jq -n --arg id ${id} --rawfile text ${refs.${id}}/ref.txt \
            '{name: $id, profile_id: $id, ref_audio_filename: "ref.wav", ref_text: $text, x_vector_only_mode: false, language: "Auto"}' > $out
        '';
      }
      {
        name = "profiles/${id}/ref.wav";
        path = "${refs.${id}}/ref.wav";
      }
    ]) cloned
  );

  # what /var/lib/models/jarvis links to
  modelTree = pkgs.linkFarm "jarvis-models" (
    [
      {
        name = "kws";
        path = models.kws;
      }
      {
        name = "silero_vad.onnx";
        path = models.silero-vad;
      }
      {
        name = "tts/base";
        path = models.tts-base;
      }
    ]
    ++ map (id: {
      name = "stt/${id}";
      path = sttModels.${id}.files;
    }) cfg.stt.models
    ++ lib.optional (models.tts-base-small != null) {
      name = "tts/base-small";
      path = models.tts-base-small;
    }
    ++ [
      {
        name = "tts/kurisu";
        path = models.tts-kurisu;
      }
      {
        name = "tts/voices";
        path = voices;
      }
    ]
    ++ map (id: {
      name = "refs/${id}";
      path = refs.${id};
    }) cloned
  );

  # the tts: no llama.cpp server for qwen3-tts (llama-server has no
  # /v1/audio/speech, llama-tts is a cli), so Qwen3-TTS-Openai-Fastapi's
  # rocm image, built here once with podman. llama-swap starts a server
  # through its unit (llama-swap is a sandboxed dynamic user, root runs
  # podman) and stops it with cmdStop
  ttsSrc = pkgs.fetchFromGitHub {
    owner = "groxaxo";
    repo = "Qwen3-TTS-Openai-Fastapi";
    rev = "783bf0e51596f1eb1e70124258f0e1ea4bf3b40a";
    hash = "sha256-8xQPUgXGF0SXMefDSiCDVw8f5g1jGmSk4lVmTE4EivY=";
  };
  # Dockerfile.rocm's FROM (rocm6.3.1, ubuntu 22.04, py3.12, torch 2.6.0)
  # was never published. this one has the same python and torch, and is
  # 10 GB instead of 24 for rocm 6
  ttsBase = "docker.io/rocm/pytorch:rocm7.1_ubuntu24.04_py3.12_pytorch_release_2.6.0";
  ttsImage = "localhost/jarvis-tts:${builtins.substring 0 7 ttsSrc.rev}-rocm7.1";

  # the same ports as proxy in the llama-swap entries (readme)
  ttsServers = {
    base = {
      port = 8881;
      path = models.tts-base;
      type = "base";
      speakers = [ ];
    };
    kurisu = {
      port = 8882;
      path = models.tts-kurisu;
      type = "customvoice";
      speakers = [
        "kurisu"
        "amadeus"
      ];
    };
  }
  // lib.optionalAttrs (models.tts-base-small != null) {
    # the 0.6b base, the same voice library as base
    small = {
      port = 8883;
      path = models.tts-base-small;
      type = "base";
      speakers = [ ];
    };
  };

  ttsConfig =
    name: s:
    (pkgs.formats.yaml { }).generate "jarvis-tts-${name}.yaml" {
      default_model = name;
      models.${name} = {
        hf_id = "${s.path}";
        inherit (s) type;
      };
      # no torch.compile: on rocm its warmup compiled for over 3.5 minutes on
      # every cold load (the kernel cache dies with the container) and never
      # finished. without it the server skips its fast streaming paths too,
      # so the 1.7b voice comes at ~0.7x real time (RTF 1.45): the daemon
      # waits for enough of it before it plays, see audio.PlayStream.
      # 72 frames is the readme's decode window for amd
      optimization = {
        attention = "flash_attention_2";
        use_compile = false;
        use_cuda_graphs = false;
        use_fast_codebook = true;
        streaming = {
          decode_window_frames = 72;
          emit_every_frames = 6;
        };
      };
      server = {
        host = "127.0.0.1";
        inherit (s) port;
      };
      voices = map (v: {
        name = v;
        language = "English";
      }) s.speakers;
    };

  # llama-swap's cmd: start the unit, live while it runs
  jarvis-tts = pkgs.writeShellApplication {
    name = "jarvis-tts";
    runtimeInputs = [ pkgs.systemd ];
    text = ''
      unit="jarvis-tts-$1.service"
      trap 'systemctl stop "$unit"; exit 0' TERM INT
      systemctl start "$unit"
      while systemctl is-active -q "$unit"; do
        sleep 2 &
        wait $!
      done
      exit 1
    '';
  };

  # no rounded shapes in jarvis' qml, ever
  noRadius = pkgs.runCommandLocal "jarvis-no-radius" { } ''
    if grep -n radius ${lib.escapeShellArgs cfg.ui.qmlFiles} /dev/null; then
      echo "jarvis: radius in the qml above, its shapes stay square" >&2
      exit 1
    fi
    touch $out
  '';
in
{
  options.services.jarvis = {
    enable = lib.mkEnableOption "jarvis, the local voice assistant";
    package = lib.mkOption {
      type = lib.types.package;
      default = pkgs.callPackage ./package.nix { };
      description = "The jarvis daemon and cli.";
    };
    modelsDir = lib.mkOption {
      type = lib.types.str;
      default = "/var/lib/models/jarvis";
      description = "Where the downloaded models are linked, readable by the user and llama-swap.";
    };
    proxy = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      example = "http://127.0.0.1:10808";
      description = "A proxy for the hugging face files, the tts image's build (docker hub, pip) and nothing else.";
    };
    character = lib.mkOption {
      type = lib.types.str;
      default = "jarvis";
      description = "The character at the start, a folder in characters/. The menu's pick wins after.";
    };
    llms = lib.mkOption {
      type = lib.types.attrsOf lib.types.str;
      default = {
        jarvis = "jarvis";
      };
      example = {
        jarvis = "Gemma 4 12B";
        jarvis-fast = "Qwen3.5 4B";
      };
      description = "The llama-swap models the characters can think with, and their names in the menu.";
    };
    llm = lib.mkOption {
      type = lib.types.str;
      default = "jarvis";
      description = "The llm at the start, one of llms. The menu's pick wins after.";
    };
    stt.models = lib.mkOption {
      type = lib.types.listOf (lib.types.enum (lib.attrNames sttModels));
      default = [
        "parakeet-v3"
        "gigaam-v2"
        "whisper-small"
      ];
      description = "The speech to text models to download and offer in the menu, in its order.";
    };
    stt.model = lib.mkOption {
      type = lib.types.enum (lib.attrNames sttModels);
      default = "parakeet-v3";
      description = "The speech to text model at the start, one of stt.models. The menu's pick wins after.";
    };
    wakeWord.enable = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = "Listen for the active character's wake phrase.";
    };
    audio.source = lib.mkOption {
      type = lib.types.str;
      default = "default";
      description = "The input device. The menu's pick wins after.";
    };
    history.turns = lib.mkOption {
      type = lib.types.int;
      default = 6;
      description = "Exchanges kept for the next question.";
    };
    notify = lib.mkOption {
      type = lib.types.bool;
      # off: the menu comes back when jarvis starts speaking, with the answer
      default = false;
      description = "A notification with the answer while the menu is closed.";
    };
    llamaSwapUrl = lib.mkOption {
      type = lib.types.str;
      default = "http://127.0.0.1:11434";
      description = "llama-swap, it serves the llm and the tts.";
    };
    # what the llm may use, only when the user asks for it. null turns one off
    tools.webSearch = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default =
        if config.services.searx.enable then
          "http://127.0.0.1:${toString (config.services.searx.settings.server.port or 8888)}"
        else
          null;
      description = "searxng for the web search, its json api. The local one when it's on.";
    };
    tools.apps = lib.mkOption {
      type = lib.types.bool;
      default = true;
      description = "open_app: \"open steam\" starts an installed app (its desktop entry), as its own systemd user unit.";
    };
    tools.terminal = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      example = "kitty";
      description = "What runs the terminal apps open_app starts (it gets -e and the command), null leaves them out.";
    };
    tools.notesMcp = lib.mkOption {
      type = lib.types.nullOr lib.types.str;
      default = null;
      example = "http://127.0.0.1:6281/mcp";
      description = "A docs-mcp-server with the user's notes in its library \"notes\".";
    };
    openCmd = lib.mkOption {
      type = lib.types.listOf lib.types.str;
      default = [
        "qs"
        "-c"
        "hyprland"
        "ipc"
        "call"
        "menu"
        "open"
        "jarvis"
      ];
      description = "What the wake word runs to open the menu.";
    };
    path = lib.mkOption {
      type = lib.types.listOf lib.types.package;
      default = [ pkgs.quickshell ];
      description = "What openCmd needs on the daemon's PATH.";
    };
    ui.qmlFiles = lib.mkOption {
      type = lib.types.listOf lib.types.path;
      default = [ ];
      description = "The menu's qml, checked at build time: no radius in it, its shapes stay square.";
    };
  };

  config = lib.mkIf cfg.enable {
    assertions = [
      {
        assertion = lib.elem cfg.character characterDirs;
        message = "services.jarvis.character: no characters/${cfg.character}";
      }
      {
        assertion = lib.elem cfg.stt.model cfg.stt.models;
        message = "services.jarvis.stt.model: ${cfg.stt.model} isn't in services.jarvis.stt.models";
      }
      {
        assertion = cfg.llms ? ${cfg.llm};
        message = "services.jarvis.llm: ${cfg.llm} isn't in services.jarvis.llms";
      }
    ];

    environment.systemPackages = [ jarvis ];

    environment.etc = {
      "jarvis/config.toml".source = configFile;
      "jarvis/characters".source = characters;
      "jarvis/prompts".source = ./prompts;
      "jarvis/sounds".source = ./sounds;
    };

    # the models, readable by the user, llama-swap and the tts containers
    systemd.tmpfiles.rules = [
      "d /var/lib/models 0755 root root -"
      "L+ ${modelsDir} - - - - ${modelTree}"
    ];

    system.checks = [ noRadius ];

    systemd.user.services.jarvis = {
      description = "jarvis voice assistant";
      partOf = [ "graphical-session.target" ];
      after = [ "graphical-session.target" ];
      wantedBy = [ "graphical-session.target" ];
      # notify-send, pw-play and what opens the menu, from PATH
      path = [
        pkgs.libnotify
        pkgs.pipewire
      ]
      ++ cfg.path;
      serviceConfig = {
        ExecStart = "${lib.getExe jarvis} daemon";
        Restart = "on-failure";
        RestartSec = 5;
      };
    };

    virtualisation.podman.enable = true;

    systemd.services =
      lib.mapAttrs' (
        name: s:
        lib.nameValuePair "jarvis-tts-${name}" {
          description = "jarvis: qwen3-tts ${name} on :${toString s.port}";
          requires = [ "jarvis-tts-image.service" ];
          after = [ "jarvis-tts-image.service" ];
          serviceConfig = {
            ExecStart = lib.escapeShellArgs [
              podman
              "run"
              "--rm"
              "--replace"
              "--name=jarvis-tts-${name}"
              "--network=host"
              "--ipc=host"
              "--device=/dev/kfd"
              "--device=/dev/dri"
              "--security-opt=seccomp=unconfined"
              # the dgpu only, like llama-swap
              "-e"
              "HIP_VISIBLE_DEVICES=0"
              "-e"
              "HOST=127.0.0.1"
              "-e"
              "PORT=${toString s.port}"
              "-e"
              "TTS_BACKEND=optimized"
              "-e"
              "TTS_CONFIG=/etc/jarvis-tts.yaml"
              "-e"
              "VOICE_LIBRARY_DIR=${voices}"
              "-e"
              "TTS_WARMUP_ON_START=false"
              "-e"
              "GPU_KEEPALIVE_INTERVAL=0"
              "-e"
              "HF_HUB_OFFLINE=1"
              # glibc keeps what torch frees while loading in its arenas,
              # gigabytes of ram for nothing: fewer arenas, given back sooner
              "-e"
              "MALLOC_ARENA_MAX=2"
              "-e"
              "MALLOC_TRIM_THRESHOLD_=67108864"
              "-v"
              "/nix/store:/nix/store:ro"
              "-v"
              "${ttsConfig name s}:/etc/jarvis-tts.yaml:ro"
              ttsImage
            ];
            ExecStop = "${podman} stop -t 10 jarvis-tts-${name}";
          };
        }
      ) ttsServers
      // {
        # the image is big (rocm pytorch), built once per pinned commit
        jarvis-tts-image = {
          description = "jarvis: build the qwen3-tts server image";
          after = [ "network-online.target" ];
          wants = [ "network-online.target" ];
          # the dockerfile's FROM is a short name (rocm/pytorch), this host has
          # no registries.conf to say where those live
          environment = {
            CONTAINERS_REGISTRIES_CONF = pkgs.writeText "jarvis-registries.conf" ''
              unqualified-search-registries = ["docker.io"]
            '';
          }
          // lib.optionalAttrs (proxy != null) {
            # podman pulls through it, and hands it to the build's RUN steps
            # (pip, git): --network=host, so a proxy on 127.0.0.1 still works
            HTTPS_PROXY = proxy;
            HTTP_PROXY = proxy;
            https_proxy = proxy;
            http_proxy = proxy;
            NO_PROXY = "127.0.0.1,localhost";
          };
          serviceConfig = {
            Type = "oneshot";
            RemainAfterExit = true;
            TimeoutStartSec = "3h";
          };
          script = ''
            ${podman} image exists ${ttsImage} ||
              ${podman} build --network=host --from ${ttsBase} -t ${ttsImage} -f Dockerfile.rocm ${ttsSrc}
          '';
        };

      }
      // lib.optionalAttrs (config.services ? llama-swap) {
        # jarvis-tts, and systemctl for cmdStop
        llama-swap.path = lib.mkIf config.services.llama-swap.enable [
          jarvis-tts
          pkgs.systemd
        ];
      };

    # llama-swap starts and stops the tts units without a password
    security.polkit.extraConfig = ''
      polkit.addRule(function (action, subject) {
        if (action.id == "org.freedesktop.systemd1.manage-units" &&
            subject.user == "llama-swap" &&
            /^jarvis-tts-[a-z]+\.service$/.test(action.lookup("unit"))) {
          var verb = action.lookup("verb");
          if (verb == "start" || verb == "stop")
            return polkit.Result.YES;
        }
      });
    '';
  };
}
