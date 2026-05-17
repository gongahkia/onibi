import ognl.Ognl;

class DemoController {
    Object handler(javax.servlet.http.HttpServletRequest request, Object ctx, Object root) throws Exception {
        return Ognl.getValue(request.getParameter("expr"), ctx, root);
    }
}
