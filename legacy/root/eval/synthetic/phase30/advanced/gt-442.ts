const billing = {
  charge(_user: string, _amount: number) {
  },
};

export function checkout(req: { user: string; body: { itemId: string; price: number; quantity: number } }) {
  const { itemId, price, quantity } = req.body;
  const total = price * quantity;
  billing.charge(req.user, total); // sink
  return { itemId, total };
}
