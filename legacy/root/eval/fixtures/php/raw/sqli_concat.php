<?php
$conn = mysqli_connect("localhost", "root", "", "app");
$id = $_GET['id'];
$query = "SELECT * FROM users WHERE id=" . $id;
mysqli_query($conn, $query);
