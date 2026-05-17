import jakarta.servlet.http.HttpServlet;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import jakarta.servlet.annotation.WebServlet;

@WebServlet("/attachment")
class Gt423Servlet extends HttpServlet {
    protected void doGet(HttpServletRequest req, HttpServletResponse resp) {
        String file = req.getParameter("file");
        resp.setHeader("Content-Disposition", "attachment; filename=" + file); // sink
    }
}
