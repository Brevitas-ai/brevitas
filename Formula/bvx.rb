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
      sha256 "8bf024eedeb288afeeef6e830f4f48afeaacec3a9103758d67039922ea757873"
    end
    on_intel do
      url "https://github.com/Brevitas-ai/brevitas/releases/download/v0.1.1/bvx-0.1.1-darwin-amd64.tar.gz"
      sha256 "86a5b054a1fdd22069cfb215fbef3a43cfaf74bd1ab811ca3e686cd05408bc02"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/Brevitas-ai/brevitas/releases/download/v0.1.1/bvx-0.1.1-linux-arm64.tar.gz"
      sha256 "5044fc4f3ca36bdd667f5f43620c4e440817faeb84e8ddb8c8c19457ea8a194a"
    end
    on_intel do
      url "https://github.com/Brevitas-ai/brevitas/releases/download/v0.1.1/bvx-0.1.1-linux-amd64.tar.gz"
      sha256 "ac1333f92b26a9ecca1ee2a282aea5932e35fe8d61bce1d16a93c191ea2877f8"
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
