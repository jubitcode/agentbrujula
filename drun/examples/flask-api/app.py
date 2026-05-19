"""Simple Flask API example for drun."""

from flask import Flask, jsonify
from flask_restful import Api, Resource

app = Flask(__name__)
api = Api(app)

# In-memory data store
items = {
    1: {"name": "Item 1", "description": "First item"},
    2: {"name": "Item 2", "description": "Second item"},
}


class ItemList(Resource):
    """List all items or create a new one."""

    def get(self):
        return jsonify({"items": list(items.values())})


class Item(Resource):
    """Get, update, or delete a specific item."""

    def get(self, item_id):
        if item_id not in items:
            return {"error": "Item not found"}, 404
        return jsonify(items[item_id])


class HealthCheck(Resource):
    """Health check endpoint."""

    def get(self):
        return {"status": "healthy", "service": "flask-api"}


api.add_resource(ItemList, "/items")
api.add_resource(Item, "/items/<int:item_id>")
api.add_resource(HealthCheck, "/health")


@app.route("/")
def index():
    """Root endpoint."""
    return jsonify({
        "message": "Welcome to Flask API",
        "endpoints": ["/items", "/items/<id>", "/health"],
    })


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=5000, debug=True)
