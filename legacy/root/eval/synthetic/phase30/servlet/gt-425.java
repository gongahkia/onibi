import java.sql.Connection;
import java.sql.Statement;
import jakarta.servlet.http.HttpServlet;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import jakarta.servlet.annotation.WebServlet;

@WebServlet("/orders")
class Gt425Servlet extends HttpServlet {
    private Connection conn;

    protected void doGet(HttpServletRequest req, HttpServletResponse resp) throws java.io.IOException {
        try {
            String sort = req.getParameter("sort");
            Statement stmt = conn.createStatement();
            stmt.executeQuery("SELECT * FROM orders ORDER BY " + sort); // sink
        } catch (Exception ignored) {
        }
    }
}
