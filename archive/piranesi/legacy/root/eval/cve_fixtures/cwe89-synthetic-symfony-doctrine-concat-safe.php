<?php

function findUsersByName($conn, $name) {
    $sql = "SELECT id, email FROM users WHERE name = :name";
    return $conn->executeQuery($sql, ["name" => $name])->fetchAllAssociative();
}

