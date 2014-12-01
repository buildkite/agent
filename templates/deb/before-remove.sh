if ( initctl status buildbox-agent | grep start ); then
  service buildbox-agent stop || true
fi
