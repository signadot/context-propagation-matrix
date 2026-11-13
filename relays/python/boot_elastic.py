"""Boot file for the elastic target: attach Elastic APM, then run the relay.

Elastic APM has no zero-code runner for Python (nothing like ddtrace-run), so
its documented setup runs here: elasticapm.instrument() to patch libraries and
the Flask middleware on the app object. app.py is the same uninstrumented
application in every image; this file is only copied into the elastic one.

Importing app runs its module-level imports and definitions, the setup below
then follows, and the gRPC server, the consumers and Flask start last
(app.main()), the same order the relay had when the setup lived in app.py.
"""

# The application first, so its libraries (grpc, requests, confluent_kafka,
# pika, flask) are imported before elastic-apm, as when the setup was in app.py.
import app as relay

import elasticapm  # noqa: E402
from elasticapm.contrib.flask import ElasticAPM  # noqa: E402

elasticapm.instrument()
ElasticAPM(relay.app)

if __name__ == "__main__":
    relay.main()
