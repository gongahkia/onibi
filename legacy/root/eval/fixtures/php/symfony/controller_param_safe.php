<?php
$conn->executeQuery("SELECT * FROM users WHERE id = ?", [$request->get('id')]);
