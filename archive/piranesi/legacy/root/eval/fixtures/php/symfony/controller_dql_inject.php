<?php
$id = $request->get('id');
$conn->executeQuery("SELECT * FROM users WHERE id=" . $id);
