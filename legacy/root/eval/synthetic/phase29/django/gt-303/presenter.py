from markupsafe import Markup

def render_notice(value: str):
    markup = f"<p class='notice'>{value}</p>"
    return Markup(markup)  # SINK
