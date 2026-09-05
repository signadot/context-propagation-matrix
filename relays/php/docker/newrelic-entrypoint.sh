#!/bin/sh
# New Relic target only: the PHP agent reads its license from ini, which
# cannot be baked into the image, so write it from the environment at
# startup (the agent's documented container pattern), then hand off to the
# shared entrypoint. Applies to both Apache (mod_php) and the CLI workers,
# which load the same conf.d.
set -u

cat > /usr/local/etc/php/conf.d/zz-newrelic-env.ini <<EOF
newrelic.license = "${NEW_RELIC_LICENSE_KEY:-}"
newrelic.appname = "${NEW_RELIC_APP_NAME:-relay-newrelic-php}"
newrelic.distributed_tracing_enabled = true
newrelic.logfile = "/tmp/newrelic-agent.log"
newrelic.daemon.logfile = "/tmp/newrelic-daemon.log"
EOF

exec /entrypoint.sh
