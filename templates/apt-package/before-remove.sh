if ( initctl status buildkite-agent | grep start ); then
  service buildkite-agent stop || true
fi
