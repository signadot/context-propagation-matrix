<?php

declare(strict_types=1);

namespace Relay;

use Composer\InstalledVersions;

/** Builds the GET /info payload the driver stamps into results. */
final class Info
{
    /**
     * OpenTelemetry's own Composer packages underneath the SDK and the
     * instrumentation packages. The Dockerfile does not name them and nothing
     * locks them, so Composer installs the newest release each build: two
     * builds of one commit can differ in them and in nothing else.
     */
    private const OTEL_UNPINNED = [
        'open-telemetry/api',
        'open-telemetry/context',
        'open-telemetry/sem-conv',
    ];

    /** @return array<string, mixed> */
    public static function build(): array
    {
        $tool = Config::tool();
        $tracer = match ($tool) {
            'otel' => sprintf(
                'open-telemetry/sdk %s (ext-opentelemetry %s)',
                self::composerVersion('open-telemetry/sdk'),
                phpversion('opentelemetry') ?: 'absent',
            ),
            'datadog' => 'ddtrace ' . (phpversion('ddtrace') ?: 'absent'),
            'elastic' => 'elastic_apm ' . (phpversion('elastic_apm') ?: 'absent'),
            'newrelic' => 'newrelic ' . (phpversion('newrelic') ?: 'absent'),
            'none' => 'none',
            default => 'unknown',
        };

        $config = [];
        foreach (getenv() as $key => $value) {
            if ($key === 'NEW_RELIC_LICENSE_KEY') {
                continue; // never echo the secret
            }
            if (preg_match('/^(DD_|OTEL_|ELASTIC_|NEW_RELIC_)/', $key)) {
                $config[$key] = $value;
            }
        }
        ksort($config);

        // A tool's Composer instrumentation package is named in the lib
        // string of the protocol it instruments, as the Go relays name their
        // contribs ("franz-go v1.x + kotel v1.y"). These packages are pinned
        // and bumped separately from the tracer (pins.yaml), and one of them
        // can change a cell on its own. Only the otel image installs any;
        // the other images' strings are the bare client library.
        $libs = [
            'server' => self::withInstrumentation(
                'apache2 mod_php / slim ' . self::composerVersion('slim/slim'),
                'open-telemetry/opentelemetry-auto-slim',
            ),
            'http_client' => self::withInstrumentation(
                'ext-curl ' . (curl_version()['version'] ?? 'unknown'),
                'open-telemetry/opentelemetry-auto-curl',
            ),
            'grpc' => sprintf(
                'ext-grpc %s / grpc/grpc %s',
                phpversion('grpc') ?: 'absent',
                self::composerVersion('grpc/grpc'),
            ),
            'kafka' => self::withInstrumentation(
                'ext-rdkafka ' . (phpversion('rdkafka') ?: 'absent'),
                'open-telemetry/opentelemetry-auto-ext-rdkafka',
            ),
            'rabbitmq' => 'php-amqplib ' . self::composerVersion('php-amqplib/php-amqplib'),
        ];

        // Named after the Rust relays' entry for the OpenTelemetry crates
        // they link. Absent where none of the packages is installed.
        $unpinned = [];
        foreach (self::OTEL_UNPINNED as $package) {
            if (InstalledVersions::isInstalled($package)) {
                $unpinned[] = $package . ' ' . self::composerVersion($package);
            }
        }
        if ($unpinned !== []) {
            $libs['propagation'] = implode(' + ', $unpinned);
        }

        return [
            'variant' => Config::variant(),
            'tool' => $tool,
            'language' => 'php',
            'tracer_version' => $tracer,
            'runtime_version' => 'php ' . PHP_VERSION,
            'config' => (object) $config,
            'libs' => $libs,
        ];
    }

    /**
     * $lib, followed by the instrumentation package and the version Composer
     * installed when the package is installed in this image. Read from
     * Composer's installed set at request time, never from a build argument,
     * so it is what the relay runs with.
     */
    private static function withInstrumentation(string $lib, string $package): string
    {
        if (!InstalledVersions::isInstalled($package)) {
            return $lib;
        }
        return $lib . ' + ' . $package . ' ' . self::composerVersion($package);
    }

    private static function composerVersion(string $package): string
    {
        try {
            return InstalledVersions::getPrettyVersion($package) ?? 'unknown';
        } catch (\Throwable) {
            return 'unknown';
        }
    }
}
