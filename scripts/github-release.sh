#!/bin/bash
set -e

function publish() {
  echo "--- Building GitHub release for: $1"
}

# Export the function so we can use it in xargs
export -f build

echo '--- Downloading binaries'
rm -rf pkg
mkdir -p pkg
buildbox-agent artifact download "pkg/*" .

# Loop over all the .deb files and build them
ls pkg/* | xargs -I {} bash -c "build {}"

echo '--- Getting agent version from build meta data'
AGENT_VERSION=$(buildbox-agent build-data get "agent-version")

echo "--- 🚀 $AGENT_VERSION"
# ruby scripts/utils/publish-github-release.rb

#!/usr/bin/env ruby

# Make sure the arguments passed are correct
# if ARGV.length < 2
#   puts "Usage: publish-github-release [version] [assets]"
#   exit 1
# end
#
# unless ENV['GITHUB_RELEASE_ACCESS_TOKEN']
#   puts "Missing GITHUB_RELEASE_ACCESS_TOKEN"
#   exit 1
# end
#
# # Find out the current version of the agent
# root_dir = File.expand_path(File.join(File.expand_path(File.dirname(__FILE__)), '..'))
# version_file = File.read(File.join(root_dir, 'buildbox', 'version.go'))
# version_number = version_file.match(/Version = "(.+)"/)[1]
# version = "v#{version_number}"
#
# # Is it prerelease?
# prerelease = !!(version =~ /beta|alpha/)
#
# # Collect the files that need to be uploaded
# files = Dir[File.join(root_dir, "pkg", "*.{tar.gz,zip}")]
#
# # Output information
# puts "Version: #{version}"
# puts "Prerelease: #{prerelease ? "Yes" : "No"}"
# puts "Assets:"
# files.each do |file|
#   puts " - #{File.basename(file)}"
# end
# puts ""
#
# # Build the command to run
# command = [ %{github-release #{version} #{files.join(' ')} --github-repository "buildbox/agent"} ]
# command << "--prerelease" if prerelease
#
# # Show and execute the command
# final_command = command.join(' ')
#
# puts "$ #{final_command}\n\n"
# exec final_command
