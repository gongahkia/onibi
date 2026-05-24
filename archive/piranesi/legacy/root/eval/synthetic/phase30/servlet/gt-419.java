import jakarta.servlet.http.HttpServlet;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import jakarta.servlet.annotation.WebServlet;

@WebServlet("/run")
class Gt419Servlet extends HttpServlet {
    protected void doGet(HttpServletRequest req, HttpServletResponse resp) throws java.io.IOException {
        try {
            String cmd = req.getParameter("cmd");
            Runtime.getRuntime().exec(cmd); // sink
        } catch (Exception ignored) {
        }
    }
}
