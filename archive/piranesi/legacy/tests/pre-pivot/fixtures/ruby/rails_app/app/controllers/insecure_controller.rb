class InsecureController < ApplicationController
  skip_before_action :verify_authenticity_token, only: :webhook

  def search
    User.where("name = '#{params[:name]}'")
  end

  def report
    User.find_by_sql("SELECT * FROM users WHERE id = #{params[:id]}")
  end

  def create
    User.create(params.require(:user).permit!)
  end

  def create_safe
    User.create(params.require(:user).permit(:name, :email))
  end

  def render_action
    render params[:action]
  end

  def render_safe
    render "users/show"
  end

  def shell
    system("convert #{params[:file]}")
  end

  def file_read
    File.read(params[:path])
  end

  def yaml_load
    YAML.load(request.body.read)
  end

  def yaml_safe_load
    YAML.safe_load(request.body.read)
  end

  def safe_where
    User.where(name: params[:name])
  end

  def safe_query
    User.where("name = ?", params[:name])
  end
end
