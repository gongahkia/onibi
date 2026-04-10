package com.example.demo;

import java.util.List;
import org.springframework.data.jpa.repository.JpaRepository;
import org.springframework.data.jpa.repository.Query;

interface UserRepository extends JpaRepository<UserEntity, Long> {
    @Query(value = "SELECT * " + "FROM users WHERE active = true", nativeQuery = true)
    List<UserEntity> findActiveUsers();
}
