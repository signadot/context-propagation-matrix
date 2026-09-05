<?php

declare(strict_types=1);

namespace Relay;

use Composer\InstalledVersions;

/** Builds the GET /info payload the driver stamps into results. */
final class Info
{
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
            default => 'unknown',
        };

        $config = [];
        foreach (getenv() as $key => $value) {
            if (preg_match('/^(DD_|OTEL_|ELASTIC_)/', $key)) {
                $config[$key] = $value;
            }
        }
        ksort($config);

        return [
            'variant' => Config::variant(),
            'tool' => $tool,
            'language' => 'php',
            'tracer_version' => $tracer,
            'runtime_version' => 'php ' . PHP_VERSION,
            'config' => (object) $config,
            'libs' => [
                'server' => 'apache2 mod_php / slim ' . self::composerVersion('slim/slim'),
                'http_client' => 'ext-curl ' . (curl_version()['version'] ?? 'unknown'),
                'grpc' => sprintf(
                    'ext-grpc %s / grpc/grpc %s',
                    phpversion('grpc') ?: 'absent',
                    self::composerVersion('grpc/grpc'),
                ),
                'kafka' => 'ext-rdkafka ' . (phpversion('rdkafka') ?: 'absent'),
                'rabbitmq' => 'php-amqplib ' . self::composerVersion('php-amqplib/php-amqplib'),
            ],
        ];
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
