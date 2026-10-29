# Homebrew formula for Mailer: installs the `mailer` command, which runs
# Mailer's container with Docker.
#
# This repo doubles as its own tap:
#
#   brew tap krispowers/mailer https://github.com/KrisPowers/mailer
#   brew install krispowers/mailer/mailer
#
# When you cut a release, set `tag` to the new version.
class Mailer < Formula
  desc "Self-hosted mail server for your own domains, with zero open ports"
  homepage "https://github.com/KrisPowers/mailer"
  url "https://github.com/KrisPowers/mailer.git", tag: "v0.1.0"
  license "MIT"
  head "https://github.com/KrisPowers/mailer.git", branch: "main"

  def install
    libexec.install "scripts/mailer"
    pkgshare.install "compose.yaml"
    (bin/"mailer").write_env_script libexec/"mailer", MAILER_SHARE: pkgshare
  end

  def caveats
    <<~EOS
      Mailer runs in Docker: Docker Desktop on macOS, or Docker Engine with
      Compose v2 on Linux. Then:
        mailer start
      and open http://localhost:8080 to finish setup.
    EOS
  end

  test do
    assert_match "mailer start", shell_output("#{bin}/mailer help")
    assert_path_exists pkgshare/"compose.yaml"
  end
end
