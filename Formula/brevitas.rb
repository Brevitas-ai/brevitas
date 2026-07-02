# Homebrew formula for the Brevitas installer CLI.
#
# This installs a PREBUILT binary per platform — no Go toolchain or Xcode
# Command Line Tools are required. `brew install --HEAD` still builds from
# source for those who want the bleeding edge.
#
# The optimization logic (brevitas-systems) is a separate Python package and is
# NOT bundled here — `brevitas install` / `brevitas update` manage it via pip.
class Brevitas < Formula
  desc "Middleware installer that routes AI coding assistants through Brevitas"
  homepage "https://github.com/Brevitas-ai/brevitas"
  version "0.1.0"
  license "MIT"

  on_macos do
    on_arm do
      url "https://github.com/Brevitas-ai/brevitas/releases/download/v0.1.0/brevitas-0.1.0-darwin-arm64.tar.gz"
      sha256 "6d051e677a09ac1b3ad6f2194f27d49e4335a07818e799b0f07bddf6aa9ddbe1"
    end
    on_intel do
      url "https://github.com/Brevitas-ai/brevitas/releases/download/v0.1.0/brevitas-0.1.0-darwin-amd64.tar.gz"
      sha256 "9514950771b4284ca6a464380ecab87cda3950016b3b3b0e188533bdb57474e0"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/Brevitas-ai/brevitas/releases/download/v0.1.0/brevitas-0.1.0-linux-arm64.tar.gz"
      sha256 "366dce87f1ed74a04db864b0ae4f707f63a147a562ee7815b5a0fcfb82fc1230"
    end
    on_intel do
      url "https://github.com/Brevitas-ai/brevitas/releases/download/v0.1.0/brevitas-0.1.0-linux-amd64.tar.gz"
      sha256 "0873902be89d32f38405dae34fec42be7c7f72fac532732c71c3857c6844fa69"
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
      system "go", "build", *std_go_args(ldflags: ldflags, output: bin/"brevitas"), "./cmd/brevitas"
    else
      bin.install "brevitas"
    end
  end

  def caveats
    <<~EOS
      Brevitas configures your AI coding tools to route through a local proxy.

      Next steps:
        brevitas install     # detect tools, store your API key, configure, start

      The optimization engine (brevitas-systems) is a Python package:
        pip install brevitas-systems
        brevitas update      # keep it current
    EOS
  end

  service do
    run [opt_bin/"brevitas", "serve"]
    keep_alive true
    log_path var/"log/brevitas/proxy.out.log"
    error_log_path var/"log/brevitas/proxy.err.log"
  end

  test do
    assert_match "brevitas", shell_output("#{bin}/brevitas version")
    assert_match "Commands:", shell_output("#{bin}/brevitas help")
  end
end
