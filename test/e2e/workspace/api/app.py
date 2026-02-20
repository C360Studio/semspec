from flask import Flask, jsonify

app = Flask(__name__)


@app.route("/hello")
def hello():
    return jsonify({"message": "Hello World"})


if __name__ == "__main__":
    app.run(port=5000)
