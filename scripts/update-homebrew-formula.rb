#!/usr/bin/env ruby

# Reads the formula from STDIN and prints an updated version

# The formula has these sections, which need to be updated:
#
#   stable do
#     version "..."
#     if Hardware::CPU.arm?
#       url     "..."
#       sha256  "..."
#     else
#       url     "..."
#       sha256  "..."
#     end
#   end
#
#   devel do
#     version "..."
#     if Hardware::CPU.arm?
#       url     "..."
#       sha256  "..."
#     else
#       url     "..."
#       sha256  "..."
#     end
#   end

release, version, amd64_url, amd64_sha256, arm64_url, arm64_sha256 = ARGV

print $stdin.read.sub(%r{
  (
    #{release} \s+ do      .*?
      version \s+ ").*?("  .*?
      if \s+ Hardware::CPU.arm\? .*?
        url     \s+ ").*?("  .*?
        sha256  \s+ ").*?("  .*?
      else .*?
        url     \s+ ").*?("  .*?
        sha256  \s+ ").*?("  .*?
      end .*?
    end .*?
  )
}xm, "\\1#{version}\\2#{arm64_url}\\3#{arm64_sha256}\\4#{amd64_url}\\5#{amd64_sha256}\\6")
