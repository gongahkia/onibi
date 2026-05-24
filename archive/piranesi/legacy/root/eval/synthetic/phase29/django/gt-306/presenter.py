from markupsafe import Markup, escape

def render_notice(value: str):
    safe = escape(value)
    markup = f"<p class='notice'>{safe}</p>"
    return Markup(markup)  # SINK
