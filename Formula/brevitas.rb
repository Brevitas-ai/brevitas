# Homebrew formula for the Brevitas installer CLI.
#
# The optimization logic (brevitas-systems) is a separate Python package and is
# NOT bundled here — `brevitas install` / `brevitas update` manage it via pip.
#
# For a tap: place this file at Formula/brevitas.rb in your
# homebrew-brevitas tap repo and run `brew install brevitas-systems/brevitas/brevitas`.
class Brevitas < Formula
  desc "Middleware installer that routes AI coding assistants through Brevitas"
  homepage "https://github.com/brevitas-systems/brevitas"
  license "MIT"
  head "https://github.com/brevitas-systems/brevitas.git", branch: "main"

  # For tagged releases, point url/sha256 at the release tarball:
  #   url "https://github.com/brevitas-systems/brevitas/archive/refs/tags/v0.1.0.tar.gz"
  #   sha256 "REPLACE_WITH_RELEASE_TARBALL_SHA256"
  version "0.1.0"

  depends_on "go" => :build

  def install
    ldflags = %W[
      -s -w
      -X github.com/brevitas-systems/brevitas/internal/version.Version=#{version}
      -X github.com/brevitas-systems/brevitas/internal/version.Date=#{time.iso8601}
    ]
    system "go", "build", *std_go_args(ldflags: ldflags, output: bin/"brevitas"), "./cmd/brevitas"
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
