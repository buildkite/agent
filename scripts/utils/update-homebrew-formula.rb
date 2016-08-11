#!/usr/bin/env ruby

# Reads the formula from STDIN and prints an updated version

# The formula has these sections, which need to be updated:
#
#   stable do
#     version "..."
#     url     "..."
#     sha256  "..."
#   end
#
#   devel do
#     version "..."
#     url     "..."
#     sha256  "..."
#   end

release, version, url, sha256 = ARGV

print $stdin.read.sub(%r{
  (
    #{release} \s+ do      .*?
      version \s+ ").*?("  .*?
      url     \s+ ").*?("  .*?
      sha256  \s+ ").*?("  .*?
    end
  )
}xm, "\\1#{version}\\2#{url}\\3#{sha256}\\4")
