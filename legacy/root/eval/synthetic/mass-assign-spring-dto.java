public class CreateUserDto {
    public String name;
}

public class UserController {
    @PostMapping("/users")
    public User createUser(@RequestBody CreateUserDto dto) {
        User user = new User();
        user.setName(dto.name);
        return userRepository.save(user);
    }
}
