<?php
global $wpdb;
$wpdb->get_results($wpdb->prepare("SELECT * FROM posts WHERE id=%d", $_GET['id']));
