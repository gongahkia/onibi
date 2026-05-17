import java.nio.file.Files;
import java.nio.file.Path;
import jakarta.servlet.http.HttpServlet;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.servlet.http.HttpServletResponse;
import jakarta.servlet.annotation.WebServlet;

@WebServlet("/download")
class Gt420Servlet extends HttpServlet {
    protected void doGet(HttpServletRequest req, HttpServletResponse resp) throws java.io.IOException {
        String file = req.getParameter("file");
        String body = Files.readString(Path.of("/srv/exports/" + file)); // sink
        resp.getWriter().write(body);
    }
}
