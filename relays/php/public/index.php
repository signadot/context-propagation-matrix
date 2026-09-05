<?php

// Naive relay HTTP endpoints (POST /relay, GET /info) served by Apache
// mod_php. Never touches propagation headers or tracing APIs — context
// propagation is the instrumentation's job.

declare(strict_types=1);

require __DIR__ . '/../vendor/autoload.php';

use Psr\Http\Message\ResponseInterface as Response;
use Psr\Http\Message\ServerRequestInterface as Request;
use Relay\Forwarder;
use Relay\Info;
use Slim\Factory\AppFactory;

$app = AppFactory::create();
$app->addBodyParsingMiddleware();
$app->addErrorMiddleware(true, true, true);

$app->post('/relay', function (Request $request, Response $response): Response {
    $body = (array) $request->getParsedBody();
    Forwarder::forward((string) $body['test_id'], (string) $body['forward_via']);
    $response->getBody()->write(json_encode(['status' => 'forwarded']));
    return $response->withHeader('Content-Type', 'application/json');
});

$app->get('/info', function (Request $request, Response $response): Response {
    $response->getBody()->write(json_encode(Info::build()));
    return $response->withHeader('Content-Type', 'application/json');
});

$app->run();
