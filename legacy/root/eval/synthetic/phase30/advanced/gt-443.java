import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RestController;

@RestController
class Gt443Controller {
    private final Gt443Gateway gateway = new Gt443Gateway();

    @PostMapping("/checkout")
    Gt443Order checkout(@RequestBody Gt443Order order) {
        gateway.charge(order.total); // sink
        return order;
    }
}

class Gt443Order {
    public String sku;
    public int total;
}

class Gt443Gateway {
    void charge(int total) {
    }
}
