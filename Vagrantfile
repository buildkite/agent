# -*- mode: ruby -*-
# vi: set ft=ruby :

# Vagrantfile API/syntax version. Don't touch unless you know what you're doing!
VAGRANTFILE_API_VERSION = "2"

$INSTALL_FPM = <<SCRIPT
apt-get update --fix-missing
apt-get install -y build-essential vim curl
command curl -sSL https://rvm.io/mpapis.asc | gpg --import -
curl -L https://get.rvm.io | bash -s stable
source ~/.rvm/scripts/rvm
rvm install ruby
rvm use ruby --default
rvm rubygems current
gem install fpm deb-s3
SCRIPT

Vagrant.configure(VAGRANTFILE_API_VERSION) do |config|
  config.vm.box = "hashicorp/precise64"
  config.vm.provision "shell", inline: $INSTALL_FPM
end
