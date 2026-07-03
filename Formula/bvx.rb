# Homebrew formula for the Brevitas installer CLI (command: bvx).
#
# The command is named `bvx` to avoid colliding with the brevitas-systems
# Python package's own `brevitas` CLI.
#
# This installs a PREBUILT binary per platform — no Go toolchain or Xcode
# Command Line Tools are required. `brew install --HEAD` builds from source.
#
# The optimization logic (brevitas-systems) is a separate Python package and is
# NOT bundled here — `bvx install` / `bvx update` manage it via pip.
class Bvx < Formula
  desc "Middleware installer that routes AI coding assistants through Brevitas"
  homepage "https://github.com/Brevitas-ai/brevitas"
  version "0.1.1"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/Brevitas-ai/brevitas/releases/download/v0.1.1/bvx-0.1.1-darwin-arm64.tar.gz"
      sha256 "f71b2843d4766f20306359f82c2725dce4ebd13a2961bd904f85991fed9eb200"
    end
    on_intel do
      url "https://github.com/Brevitas-ai/brevitas/releases/download/v0.1.1/bvx-0.1.1-darwin-amd64.tar.gz"
      sha256 "e0a46d7b8849e56b64fce81f538203e516e802ed0ed27a2c8cb8fc717660128c"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/Brevitas-ai/brevitas/releases/download/v0.1.1/bvx-0.1.1-linux-arm64.tar.gz"
      sha256 "5f0f4faf1bffbbf3c1d0fec1e64a1242769d77e3a59a927f22df4ce9c02ae56a"
    end
    on_intel do
      url "https://github.com/Brevitas-ai/brevitas/releases/download/v0.1.1/bvx-0.1.1-linux-amd64.tar.gz"
      sha256 "6787158a9d76ce709b01abe721677efa3a5a760024dc2a32cfce51c6cbc96224"
    end
  end

  # Bleeding-edge source build (requires Go, only used with --HEAD).
  head do
    url "https://github.com/Brevitas-ai/brevitas.git", branch: "main"
    depends_on "go" => :build
  end

  def install
    if build.head?
      ldflags = %W[
        -s -w
        -X github.com/Brevitas-ai/brevitas/internal/version.Version=HEAD
        -X github.com/Brevitas-ai/brevitas/internal/version.Date=#{time.iso8601}
      ]
      system "go", "build", *std_go_args(ldflags: ldflags, output: bin/"bvx"), "./cmd/bvx"
    else
      bin.install "bvx"
    end
  end

  def caveats
    <<~EOS
      Brevitas configures your AI coding tools to route through a local proxy.

      Next steps:
        bvx install     # detect tools, store your API key, configure, start

      The optimization engine (brevitas-systems) is a Python package:
        pip install brevitas-systems
        bvx update      # keep it current
    EOS
  end

  service do
    run [opt_bin/"bvx", "serve"]
    keep_alive true
    log_path var/"log/brevitas/proxy.out.log"
    error_log_path var/"log/brevitas/proxy.err.log"
  end

  test do
    assert_match "brevitas", shell_output("#{bin}/bvx version")
    assert_match "Commands:", shell_output("#{bin}/bvx help")
  end
end
