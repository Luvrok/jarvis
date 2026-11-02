# the daemon and the cli in one binary: go with cgo, sherpa-onnx's c api for
# the speech parts, miniaudio (malgo) for the sound
{
  lib,
  buildGoModule,
  sherpa-onnx,
  libpulseaudio,
  patchelf,
}:

buildGoModule {
  pname = "jarvis";
  version = "0.1";
  src = ./daemon;
  vendorHash = "sha256-nqwWB7qzj522G0OKHsuZTCgPbJV3kr+3xwqyZcTdN08=";
  subPackages = [ "cmd/jarvis" ];
  env.CGO_ENABLED = 1;
  buildInputs = [ sherpa-onnx ];
  nativeBuildInputs = [ patchelf ];
  # miniaudio opens libpulse at runtime, by name
  postFixup = ''
    patchelf --add-rpath ${lib.getLib libpulseaudio}/lib $out/bin/jarvis
  '';
  meta = {
    description = "A local voice assistant: whisper, an llm and qwen3-tts behind llama-swap";
    mainProgram = "jarvis";
    platforms = lib.platforms.linux;
  };
}
