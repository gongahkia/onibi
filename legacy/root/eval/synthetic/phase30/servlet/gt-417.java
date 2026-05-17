import java.sql.Connection;
import java.sql.Statement;
import jakarta.servlet.http.HttpServlet;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import jakarta.servlet.annotation.WebServlet;

@WebServlet("/user")
class Gt417Servlet extends HttpServlet {
    private Connection conn;

    protected void doGet(HttpServletRequest req, HttpServletResponse resp) throws java.io.IOException {
        try {
            String id = req.getParameter("id");
            Statement stmt = conn.createStatement();
            stmt.executeQuery("SELECT * FROM users WHERE id = '" + id + "'"); // sink
        } catch (Exception ignored) {
        }
    }
}
