# frozen_string_literal: true

require 'json'
require 'sinatra/base'

# HTTP endpoints on :8080 (POST /relay, GET /info).
class HttpApp < Sinatra::Base
  set :show_exceptions, false

  post '/relay' do
    req = JSON.parse(request.body.read)
    Forwarder.forward(req['test_id'], req['forward_via'])
    content_type :json
    JSON.generate({ status: 'forwarded' })
  end

  get '/info' do
    content_type :json
    JSON.generate(Info.payload)
  end
end
