# -*- mode: ruby -*-
# vi: set ft=ruby :

# Vagrantfile API/syntax version. Don't touch unless you know what you're doing!
VAGRANTFILE_API_VERSION = "2"

$INSTALL_FPM = <<SCRIPT
apt-get update --fix-missing
apt-get install -y build-essential ruby-dev vim curl
gem install fpm
SCRIPT

Vagrant.configure(VAGRANTFILE_API_VERSION) do |config|
  config.vm.box = "hashicorp/precise64"
  config.vm.provision "shell", inline: $INSTALL_FPM
end
