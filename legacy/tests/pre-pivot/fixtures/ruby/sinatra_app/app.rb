require "sinatra"

get "/run" do
  system("convert #{params[:file]}")
end

get "/yaml" do
  YAML.load(request.body.read)
end
