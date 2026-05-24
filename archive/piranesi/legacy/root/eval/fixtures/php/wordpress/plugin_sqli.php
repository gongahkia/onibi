<?php
global $wpdb;
$wpdb->query("SELECT * FROM posts WHERE id=" . $_GET['id']);
