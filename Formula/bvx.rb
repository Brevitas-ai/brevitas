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
  version "0.1.31"
  license "MIT"

  # Bleeding-edge source build (requires Go, only used with --HEAD).
  head do
    url "https://github.com/Brevitas-ai/brevitas.git", branch: "main"
    depends_on "go" => :build
  end

  depends_on "python@3.13"

  on_macos do
    on_arm do
      url "https://github.com/Brevitas-ai/brevitas/releases/download/v0.1.31/bvx-0.1.31-darwin-arm64.tar.gz"
      sha256 "4d7efd19a64dee411a7fd050a278c9a7a1d14abef959802b03009acf207f1fa9"
    end
    on_intel do
      url "https://github.com/Brevitas-ai/brevitas/releases/download/v0.1.31/bvx-0.1.31-darwin-amd64.tar.gz"
      sha256 "71ddc10c7ff1701b393ca528bca61515d5de4f4d39617caa0c1c4dd0b3d5eaac"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/Brevitas-ai/brevitas/releases/download/v0.1.31/bvx-0.1.31-linux-arm64.tar.gz"
      sha256 "a1dd4c53a2fccd5fb403e598410dcb038d9243b646182538b20b9cfde98280c6"
    end
    on_intel do
      url "https://github.com/Brevitas-ai/brevitas/releases/download/v0.1.31/bvx-0.1.31-linux-amd64.tar.gz"
      sha256 "3516a00c237d908aa050404a7e4a0a0916ed1ec41ff3e05c6a3c1357a81a3ff2"
    end
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
        bvx install     # sign in, detect tools, configure, start

      bvx installs and pins the brevitas-systems optimization engine automatically.
      Run `bvx update` later to keep it current.
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
    assert_match "bvx onboard", shell_output("#{bin}/bvx help")
  end
end
