import re
from flask import request

pattern = re.compile(request.args["pattern"])
