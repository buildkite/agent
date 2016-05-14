# Takes a list of versions like so, and returns the latest (and greatest) one:
#
# 1
# 1.0
# 1.0.0
# 1.0.0.1
# 3.0.0-beta.1-810
# 3.0.0-beta.1-811
# 3.0.0-beta.1-812
# 3.0.0-beta.2-813
# 3.0.0
# 3.1.0.2-alpha.1-814
# 3.1.0.2-beta.1-814
# 3.1.0.2

def parse(version_string)
  pattern = %r{
    # Major is required
    (?<major>\d+)
    # All these numbers are optional
    (\.(?<minor>\d+))?
    (\.(?<patch>\d+))?
    (\.(?<tiny>\d+))?
    # Pre is a string like alpha, beta, etc
    (\-(?<prerelease>[a-z]+))?
    # The rest are numbers, and we dont care if its dashes or dots
    ([\-\.](?<prerelease_major>\d+))?
    ([\-\.](?<prerelease_minor>\d+))?
    ([\-\.](?<prerelease_patch>\d+))?
    ([\-\.](?<prerelease_tiny>\d+))?
  }x

  if m = pattern.match(version_string)
    {
      # Parse all the integer strings. If it's nil, make it 0
      # (i.e. "2" becomes "2.0.0")
      major:            m[:major].to_i,
      minor:            m[:minor].to_i,
      patch:            m[:patch].to_i,
      tiny:             m[:tiny].to_i,
      prerelease:       m[:prerelease],
      prerelease_major: m[:prerelease_major].to_i,
      prerelease_minor: m[:prerelease_minor].to_i,
      prerelease_patch: m[:prerelease_patch].to_i,
      prerelease_tiny:  m[:prerelease_tiny].to_i
    }
  end
end

def sort(v1, v2)
  # We need to special case the major/minor/patch/tiny being the same, but one
  # being a prerelease. i.e. 2.3.2 > 2.3.2-beta.2 and 2.3.2-beta.2 < 2.3.2
  if v1.values.first(4) == v2.values.first(4) && (!v1[:prerelease] || !v2[:prerelease])
    v1[:prerelease] ? -1 : 1
  # Otherwise we can just let Ruby sort all the parts against both (including
  # prerelease strings as alphabetical)
  else
    v1.values <=> v2.values
  end
end

lines = STDIN.readlines.map(&:strip).compact.sort {|line_1, line_2|
  sort(parse(line_1), parse(line_2))
}

puts lines.last
