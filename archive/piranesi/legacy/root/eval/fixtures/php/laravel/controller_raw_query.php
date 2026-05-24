<?php
use Illuminate\Support\Facades\DB;

$sort = $_POST['sort'];
$users = DB::table('users')->orderByRaw($sort)->get();
