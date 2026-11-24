require "json"

version = ARGV.fetch(0)
abort "Expected a stable agent version, got #{version.inspect}" unless version.match?(/\A\d+\.\d+\.\d+\z/)

entries = $stdin.readlines.map(&:split)
hashes = %w[linux-amd64 linux-arm64 darwin-amd64 darwin-arm64].to_h do |platform|
  filename = "buildkite-agent-#{platform}-#{version}.tar.gz"
  matches = entries.select { |_, name| name == filename }
  unless matches.size == 1 && matches.first.size == 2 && matches.first.first.match?(/\A[0-9a-f]{64}\z/)
    abort "Expected exactly one SHA-256 checksum for #{filename}"
  end

  [platform, matches.first.first]
end

puts JSON.pretty_generate(version: version, sha256: hashes)
