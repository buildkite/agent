# -*- mode: ruby -*-
# vi: set ft=ruby :

# Vagrantfile API/syntax version. Don't touch unless you know what you're doing!
VAGRANTFILE_API_VERSION = "2"

$INSTALL_FPM = <<SCRIPT
apt-get update --fix-missing

apt-get install -y build-essential vim curl git-core

command curl -sSL https://rvm.io/mpapis.asc | gpg --import -
curl -L https://get.rvm.io | bash -s stable
source ~/.rvm/scripts/rvm
rvm install ruby
rvm use ruby --default
rvm rubygems current
gem install fpm deb-s3

apt-get -y --force-yes -q install mercurial
cd /tmp && curl -L -O https://storage.googleapis.com/golang/go1.3.3.linux-amd64.tar.gz
tar -C /usr/local -xzf /tmp/go1.3.3.linux-amd64.tar.gz
mkdir -own -R vagrant:vagrant /home/vagrant/.go /home/vagrant/.go/src/github.com/buildbox
cd /home/vagrant/.go/src/github.com/buildbox
ln -s /vagrant agent
chown -R vagrant:vagrant /home/vagrant/.go
echo 'export GOROOT="/usr/local/go"' >> /home/vagrant/.profile
echo 'export GOPATH="/home/vagrant/.go"' >> /home/vagrant/.profile
echo 'export PATH="/home/vagrant/.go/bin:/usr/local/go/bin:$PATH"' >> /home/vagrant/.profile
cd /usr/local/go/src && GOOS=windows GOARCH=386 ./make.bash --no-clean
cd /usr/local/go/src && GOOS=windows GOARCH=amd64 ./make.bash --no-clean
cd /usr/local/go/src && GOOS=linux GOARCH=amd64 ./make.bash --no-clean
cd /usr/local/go/src && GOOS=linux GOARCH=386 ./make.bash --no-clean
cd /usr/local/go/src && GOOS=linux GOARCH=arm ./make.bash --no-clean
cd /usr/local/go/src && GOOS=darwin GOARCH=386 ./make.bash --no-clean
cd /usr/local/go/src && GOOS=darwin GOARCH=amd64 ./make.bash --no-clean
SCRIPT

Vagrant.configure(VAGRANTFILE_API_VERSION) do |config|
  config.vm.box = "hashicorp/precise64"
  config.vm.provision "shell", inline: $INSTALL_FPM
end
