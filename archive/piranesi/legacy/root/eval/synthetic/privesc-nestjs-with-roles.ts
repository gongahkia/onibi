@Controller("admin")
@UseGuards(RolesGuard)
@Roles("admin")
export class AdminController {
  @Delete("users/:id")
  deleteUser() {
    return true;
  }
}
