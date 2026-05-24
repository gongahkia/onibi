import os
import sqlite3
import subprocess

import requests
from django.db import models
from django.db.models import F, Q
from django.http import HttpResponse


class User(models.Model):
    pass


def search(request):
    q = request.GET["q"]  # tainted url_param
    conn = sqlite3.connect("app.db")
    cursor = conn.cursor()
    cursor.execute(f"SELECT * FROM items WHERE name = '{q}'")  # sqli CWE-89
    return HttpResponse(str(cursor.fetchall()))


def create(request):
    name = request.POST["name"]  # tainted request_body
    os.system(f"echo {name}")  # cmdi CWE-78
    return HttpResponse("ok")


def proxy(request):
    url = request.GET["url"]  # tainted url_param
    return HttpResponse(requests.get(url).text)  # ssrf CWE-918


def read_file(request):
    path = request.GET["path"]  # tainted url_param
    return HttpResponse(open(path).read())  # path traversal CWE-22


def dynamic(request):
    expr = request.GET["expr"]  # tainted url_param
    return HttpResponse(str(eval(expr)))  # eval CWE-94


def header_cmd(request):
    cmd = request.headers["X-Command"]  # tainted header
    subprocess.run(cmd, shell=True)  # cmdi via header CWE-78
    return HttpResponse("done")


def safe_query(request):
    q = request.GET["q"]
    conn = sqlite3.connect("app.db")
    cursor = conn.cursor()
    cursor.execute("SELECT * FROM items WHERE name = ?", (q,))  # parameterized
    return HttpResponse(str(cursor.fetchall()))


def orm_raw(request):
    q = request.GET["q"]  # tainted url_param
    rows = User.objects.raw(f"SELECT * FROM users WHERE name = '{q}'")
    return HttpResponse(str(list(rows)))


def orm_extra(request):
    q = request.GET["q"]  # tainted url_param
    rows = User.objects.extra(where=[f"name = '{q}'"])
    return HttpResponse(str(list(rows)))


def orm_filter(request):
    q = request.GET["q"]
    rows = User.objects.filter(name=q)
    return HttpResponse(str(list(rows)))


def orm_filter_q(request):
    q = request.GET["q"]
    rows = User.objects.filter(Q(name=q))
    return HttpResponse(str(list(rows)))


def orm_filter_f(request):
    rows = User.objects.filter(score__gt=F("min_score"))
    return HttpResponse(str(list(rows)))
