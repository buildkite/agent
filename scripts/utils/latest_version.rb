#!/usr/bin/env ruby

# Reads a list of versions from STDIN and prints the highest one:
#
# echo -e "latest\n1.0.1\n1.2.1\n2.3.2-beta.2\n2.3.2-alpha.2\n3.1\n3.1-beta.1\n3.0" | ruby latest_version.rb
# => 3.1

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
      # (i.e. "2" becomes "2,0,0,0,nil,0,0,0,0")
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
  # Stable release get preferred over pre-releases, but if we just let Ruby
  # sort v1.values <=> v2.values:
  #
  # [2, 3, 2, 1, nil,     0, 0, 0, 0]
  # [2, 3, 2, 1, 'beta',  1, 1, 0, 0]
  #
  # then 2.3.2.1-beta.1 would be greater than 2.3.2.1
  #
  # So we special case 2.3.2.1 <=> 2.3.2.1-beta.1 (or vice versa) and preference the
  # version that isn't the prerelease
  if v1.values.first(4) == v2.values.first(4) && (v1[:prerelease].nil? || v2[:prerelease].nil?)
    # 2.3.2.1 <=> 2.3.2.1
    if v1[:prerelease].nil? && v2[:prerelease].nil?
      0
    # 2.3.2.1 <=> 2.3.2.1-beta.1
    elsif v1[:prerelease].nil? && v2[:prerelease]
      1
    # 2.3.2.1-beta.1 <=> 2.3.2.1
    elsif v1[:prerelease] && v2[:prerelease].nil?
      -1
    end
  # For all other cases we can just let Ruby sort all the things
  #
  # [2, 0, 0, 0, nil,     0, 0, 0, 0]
  # [2, 3, 0, 0, nil,     0, 0, 0, 0]
  # [2, 3, 0, 0, nil,     0, 0, 0, 0]
  # [2, 3, 2, 1, 'alpha', 0, 0, 0, 0]
  # [2, 3, 2, 1, 'beta',  0, 0, 0, 0]
  # [2, 3, 2, 1, 'beta',  1, 0, 0, 0]
  # [2, 3, 2, 1, 'beta',  1, 1, 0, 0]
  # [2, 3, 2, 2, nil,     0, 0, 0, 0]
  else
    v1.values <=> v2.values
  end
end

parsed_version_lines = STDIN.readlines.map(&:strip).map {|line|
  if version = parse(line)
    [line, version]
  end
}.compact

puts parsed_version_lines.sort {|(l1, v1), (l2, v2)|
  sort(v1, v2)
}.last[0]